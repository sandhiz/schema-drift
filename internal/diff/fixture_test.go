package diff

import (
	"fmt"
	"testing"

	"schema-drift/internal/model"
	"schema-drift/internal/normalizer"
)

type fixtureCol struct {
	name     string
	raw      normalizer.RawColumn
	schema   string
	table    string
	override *model.Column
}

func makeTable(engine model.Engine, schema, defaultSchema, name string, cols ...normalizer.RawColumn) model.Table {
	normCols := make([]model.Column, 0, len(cols))
	for i, rc := range cols {
		if rc.Ordinal == 0 {
			rc.Ordinal = i + 1
		}
		normCols = append(normCols, normalizer.NormalizeColumn(engine, rc))
	}
	return model.Table{
		Schema:     schema,
		LogicalKey: normalizer.BuildLogicalTableKey(engine, schema, defaultSchema, name),
		Name:       name,
		Columns:    normCols,
	}
}

func intp(v int) *int { return &v }

func TestDiffFixtureSuite(t *testing.T) {
	type expectation struct {
		hasDrift        bool
		tablesOnlyA     int
		tablesOnlyB     int
		tablesModified  int
		tablesIdentical int
		colsOnlyA       int
		colsOnlyB       int
		colsModified    int
		typeDiffs       int
		lenDiffs        int
		precDiffs       int
		nullDiffs       int
		defaultDiffs    int
		unmappedMin     int
		caseDiffMin     int
		ignoredA        int
		ignoredB        int
	}

	fixtures := []struct {
		name string
		a    model.SchemaSnapshot
		b    model.SchemaSnapshot
		opts Options
		want expectation
	}{
		{
			name: "01_identical_same_engine",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{
					makeTable(model.EnginePostgres, "public", "public", "users",
						normalizer.RawColumn{Name: "id", DataType: "bigint"},
						normalizer.RawColumn{Name: "email", DataType: "character varying", CharMaxLength: intp(120)},
					),
				},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{
					makeTable(model.EnginePostgres, "public", "public", "users",
						normalizer.RawColumn{Name: "id", DataType: "bigint"},
						normalizer.RawColumn{Name: "email", DataType: "character varying", CharMaxLength: intp(120)},
					),
				},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "02_table_only_in_a",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "legacy_logs", normalizer.RawColumn{Name: "id", DataType: "int"})},
			},
			b:    model.SchemaSnapshot{Engine: model.EnginePostgres},
			want: expectation{hasDrift: true, tablesOnlyA: 1},
		},
		{
			name: "03_table_only_in_b",
			a:    model.SchemaSnapshot{Engine: model.EnginePostgres},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "webhooks", normalizer.RawColumn{Name: "id", DataType: "int"})},
			},
			want: expectation{hasDrift: true, tablesOnlyB: 1},
		},
		{
			name: "04_mixed_only_a_only_b_and_identical",
			a: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{
					makeTable(model.EngineMySQL, "app", "app", "users", normalizer.RawColumn{Name: "id", DataType: "bigint"}),
					makeTable(model.EngineMySQL, "app", "app", "old_cache", normalizer.RawColumn{Name: "k", DataType: "varchar", CharMaxLength: intp(64)}),
				},
			},
			b: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{
					makeTable(model.EngineMySQL, "app", "app", "users", normalizer.RawColumn{Name: "id", DataType: "bigint"}),
					makeTable(model.EngineMySQL, "app", "app", "new_jobs", normalizer.RawColumn{Name: "id", DataType: "bigint"}),
				},
			},
			want: expectation{hasDrift: true, tablesOnlyA: 1, tablesOnlyB: 1, tablesIdentical: 1},
		},
		{
			name: "05_empty_tables_identical",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "empty_tbl")},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "empty_tbl")},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "06_column_only_in_a",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{
					makeTable(model.EnginePostgres, "public", "public", "accounts",
						normalizer.RawColumn{Name: "id", DataType: "bigint"},
						normalizer.RawColumn{Name: "legacy_pin", DataType: "varchar", CharMaxLength: intp(10)},
					),
				},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{
					makeTable(model.EnginePostgres, "public", "public", "accounts",
						normalizer.RawColumn{Name: "id", DataType: "bigint"},
					),
				},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsOnlyA: 1},
		},
		{
			name: "07_column_only_in_b",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{
					makeTable(model.EnginePostgres, "public", "public", "accounts",
						normalizer.RawColumn{Name: "id", DataType: "bigint"},
					),
				},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{
					makeTable(model.EnginePostgres, "public", "public", "accounts",
						normalizer.RawColumn{Name: "id", DataType: "bigint"},
						normalizer.RawColumn{Name: "mfa_enabled", DataType: "boolean"},
					),
				},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsOnlyB: 1},
		},
		{
			name: "08_both_columns_added_and_removed",
			a: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{
					makeTable(model.EngineMySQL, "app", "app", "items",
						normalizer.RawColumn{Name: "id", DataType: "int"},
						normalizer.RawColumn{Name: "sku_old", DataType: "varchar", CharMaxLength: intp(20)},
					),
				},
			},
			b: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{
					makeTable(model.EngineMySQL, "app", "app", "items",
						normalizer.RawColumn{Name: "id", DataType: "int"},
						normalizer.RawColumn{Name: "sku_v2", DataType: "varchar", CharMaxLength: intp(30)},
					),
				},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsOnlyA: 1, colsOnlyB: 1},
		},
		{
			name: "09_cross_engine_nvarchar50_vs_varchar50",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "users", normalizer.RawColumn{Name: "name", DataType: "nvarchar", MaxBytes: intp(100)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "name", DataType: "character varying", CharMaxLength: intp(50)})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "10_cross_engine_mssql_varchar_vs_mysql_varchar",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "tags", normalizer.RawColumn{Name: "slug", DataType: "varchar", MaxBytes: intp(64)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{makeTable(model.EngineMySQL, "db", "db", "tags", normalizer.RawColumn{Name: "slug", DataType: "varchar", CharMaxLength: intp(64)})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "11_cross_engine_nvarchar_max_vs_pg_text",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "posts", normalizer.RawColumn{Name: "body", DataType: "nvarchar", MaxBytes: intp(-1)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "posts", normalizer.RawColumn{Name: "body", DataType: "text"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "12_cross_engine_bit_vs_boolean",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "flags", normalizer.RawColumn{Name: "enabled", DataType: "bit"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "flags", normalizer.RawColumn{Name: "enabled", DataType: "boolean"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "13_cross_engine_mysql_tinyint1_vs_boolean",
			a: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{makeTable(model.EngineMySQL, "db", "db", "flags", normalizer.RawColumn{Name: "enabled", DataType: "tinyint", ColumnType: "tinyint(1)"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "flags", normalizer.RawColumn{Name: "enabled", DataType: "bool"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "14_cross_engine_datetime2_vs_timestamp",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "events", normalizer.RawColumn{Name: "at", DataType: "datetime2"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "events", normalizer.RawColumn{Name: "at", DataType: "timestamp without time zone"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "15_cross_engine_datetimeoffset_vs_timestamptz",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "events", normalizer.RawColumn{Name: "at", DataType: "datetimeoffset"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "events", normalizer.RawColumn{Name: "at", DataType: "timestamp with time zone"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "16_cross_engine_uniqueidentifier_vs_uuid",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "orders", normalizer.RawColumn{Name: "id", DataType: "uniqueidentifier"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "orders", normalizer.RawColumn{Name: "id", DataType: "uuid"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "17_cross_engine_int_vs_int4",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "counts", normalizer.RawColumn{Name: "qty", DataType: "int", NumericPrecision: intp(10), NumericScale: intp(0)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "counts", normalizer.RawColumn{Name: "qty", DataType: "int4"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "18_cross_engine_bigserial_vs_mysql_bigint",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "seq", normalizer.RawColumn{Name: "id", DataType: "bigserial"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{makeTable(model.EngineMySQL, "db", "db", "seq", normalizer.RawColumn{Name: "id", DataType: "bigint"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "19_cross_engine_money_vs_numeric_19_4",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "ledger", normalizer.RawColumn{Name: "bal", DataType: "money"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "ledger", normalizer.RawColumn{Name: "bal", DataType: "numeric", NumericPrecision: intp(19), NumericScale: intp(4)})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "20_cross_engine_varbinary_max_vs_bytea",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "files", normalizer.RawColumn{Name: "blob", DataType: "varbinary", MaxBytes: intp(-1)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "files", normalizer.RawColumn{Name: "blob", DataType: "bytea"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "21_cross_engine_decimal10_2_vs_numeric10_2",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "invoices", normalizer.RawColumn{Name: "amt", DataType: "decimal", NumericPrecision: intp(10), NumericScale: intp(2)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "invoices", normalizer.RawColumn{Name: "amt", DataType: "numeric", NumericPrecision: intp(10), NumericScale: intp(2)})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "22_mismatch_length_50_vs_100",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "users", normalizer.RawColumn{Name: "email", DataType: "nvarchar", MaxBytes: intp(100)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "email", DataType: "character varying", CharMaxLength: intp(100)})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, lenDiffs: 1},
		},
		{
			name: "23_mismatch_precision_10_2_vs_12_2",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "orders", normalizer.RawColumn{Name: "total", DataType: "numeric", NumericPrecision: intp(10), NumericScale: intp(2)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "orders", normalizer.RawColumn{Name: "total", DataType: "numeric", NumericPrecision: intp(12), NumericScale: intp(2)})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, precDiffs: 1},
		},
		{
			name: "24_mismatch_scale_10_2_vs_10_4",
			a: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{makeTable(model.EngineMySQL, "db", "db", "rates", normalizer.RawColumn{Name: "fx", DataType: "decimal", NumericPrecision: intp(10), NumericScale: intp(2)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{makeTable(model.EngineMySQL, "db", "db", "rates", normalizer.RawColumn{Name: "fx", DataType: "decimal", NumericPrecision: intp(10), NumericScale: intp(4)})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, precDiffs: 1},
		},
		{
			name: "25_mismatch_int_vs_bigint",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "id", DataType: "integer"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "id", DataType: "bigint"})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, typeDiffs: 1},
		},
		{
			name: "26_mismatch_float32_vs_float64",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "metrics", normalizer.RawColumn{Name: "val", DataType: "real"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "metrics", normalizer.RawColumn{Name: "val", DataType: "double precision"})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, typeDiffs: 1},
		},
		{
			name: "27_mismatch_date_vs_timestamp",
			a: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{makeTable(model.EngineMySQL, "db", "db", "users", normalizer.RawColumn{Name: "dob", DataType: "date"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "dob", DataType: "timestamp"})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, typeDiffs: 1},
		},
		{
			name: "28_mismatch_varchar50_vs_text",
			a: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{makeTable(model.EngineMySQL, "db", "db", "notes", normalizer.RawColumn{Name: "summary", DataType: "varchar", CharMaxLength: intp(50)})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "notes", normalizer.RawColumn{Name: "summary", DataType: "text"})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, typeDiffs: 1},
		},
		{
			name: "29_mismatch_nullable",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "phone", DataType: "varchar", CharMaxLength: intp(20), Nullable: false})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "phone", DataType: "varchar", CharMaxLength: intp(20), Nullable: true})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, nullDiffs: 1},
		},
		{
			name: "30_same_engine_default_mismatch",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "orders", normalizer.RawColumn{Name: "status", DataType: "varchar", CharMaxLength: intp(20), HasDefault: true, DefaultExpr: "'active'"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "orders", normalizer.RawColumn{Name: "status", DataType: "varchar", CharMaxLength: intp(20), HasDefault: true, DefaultExpr: "'pending'"})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, defaultDiffs: 1},
		},
		{
			name: "31_same_engine_wrapped_parens_default_equal",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "orders", normalizer.RawColumn{Name: "retry", DataType: "int", HasDefault: true, DefaultExpr: "((0))"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "orders", normalizer.RawColumn{Name: "retry", DataType: "int", HasDefault: true, DefaultExpr: "0"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "32_cross_engine_default_expr_difference_ignored",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "events", normalizer.RawColumn{Name: "created_at", DataType: "datetime2", HasDefault: true, DefaultExpr: "(getdate())"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "events", normalizer.RawColumn{Name: "created_at", DataType: "timestamp", HasDefault: true, DefaultExpr: "now()"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1},
		},
		{
			name: "33_cross_engine_default_presence_mismatch",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "events", normalizer.RawColumn{Name: "created_at", DataType: "datetime2", HasDefault: true, DefaultExpr: "(getdate())"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "events", normalizer.RawColumn{Name: "created_at", DataType: "timestamp", HasDefault: false})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, defaultDiffs: 1},
		},
		{
			name: "34_case_insensitive_default_does_not_fail_ci",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "Users", normalizer.RawColumn{Name: "UserID", DataType: "bigint"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "userid", DataType: "bigint"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1, caseDiffMin: 2},
		},
		{
			name: "35_strict_case_flags_drift",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "Users", normalizer.RawColumn{Name: "UserID", DataType: "bigint"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "userid", DataType: "bigint"})},
			},
			opts: Options{StrictCase: true},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, caseDiffMin: 2},
		},
		{
			name: "36_unmapped_vs_mapped_type_drift_and_notice",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{makeTable(model.EngineMSSQL, "dbo", "dbo", "org", normalizer.RawColumn{Name: "node", DataType: "hierarchyid"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "org", normalizer.RawColumn{Name: "node", DataType: "text"})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, typeDiffs: 1, unmappedMin: 1},
		},
		{
			name: "37_unmapped_same_base_type_identical_with_notice",
			a: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "docs", normalizer.RawColumn{Name: "tsv", DataType: "tsvector"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "docs", normalizer.RawColumn{Name: "tsv", DataType: "tsvector"})},
			},
			want: expectation{hasDrift: false, tablesIdentical: 1, unmappedMin: 1},
		},
		{
			name: "38_unmapped_different_base_types",
			a: model.SchemaSnapshot{
				Engine: model.EngineMySQL,
				Tables: []model.Table{makeTable(model.EngineMySQL, "db", "db", "orders", normalizer.RawColumn{Name: "state", DataType: "enum", ColumnType: "enum('open','done')"})},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{makeTable(model.EnginePostgres, "public", "public", "orders", normalizer.RawColumn{Name: "state", DataType: "USER-DEFINED", ColumnType: "order_state"})},
			},
			want: expectation{hasDrift: true, tablesModified: 1, colsModified: 1, typeDiffs: 1, unmappedMin: 1},
		},
		{
			name: "39_ignore_glob_patterns",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{
					makeTable(model.EngineMSSQL, "dbo", "dbo", "users", normalizer.RawColumn{Name: "id", DataType: "bigint"}),
					makeTable(model.EngineMSSQL, "dbo", "dbo", "temp_import", normalizer.RawColumn{Name: "id", DataType: "int"}),
					makeTable(model.EngineMSSQL, "dbo", "dbo", "audit_2026", normalizer.RawColumn{Name: "id", DataType: "int"}),
				},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{
					makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "id", DataType: "bigint"}),
					makeTable(model.EnginePostgres, "public", "public", "temp_staging", normalizer.RawColumn{Name: "id", DataType: "int"}),
				},
			},
			opts: Options{IgnorePatterns: []string{"temp_*,audit_*"}},
			want: expectation{hasDrift: false, tablesIdentical: 1, ignoredA: 2, ignoredB: 1},
		},
		{
			name: "40_non_default_schemas_kept_distinct",
			a: model.SchemaSnapshot{
				Engine: model.EngineMSSQL,
				Tables: []model.Table{
					makeTable(model.EngineMSSQL, "dbo", "dbo", "users", normalizer.RawColumn{Name: "id", DataType: "bigint"}),
					makeTable(model.EngineMSSQL, "sales", "dbo", "invoices", normalizer.RawColumn{Name: "id", DataType: "bigint"}),
				},
			},
			b: model.SchemaSnapshot{
				Engine: model.EnginePostgres,
				Tables: []model.Table{
					makeTable(model.EnginePostgres, "public", "public", "users", normalizer.RawColumn{Name: "id", DataType: "bigint"}),
					makeTable(model.EnginePostgres, "billing", "public", "invoices", normalizer.RawColumn{Name: "id", DataType: "bigint"}),
				},
			},
			want: expectation{hasDrift: true, tablesOnlyA: 1, tablesOnlyB: 1, tablesIdentical: 1},
		},
	}

	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := Compare(tc.a, tc.b, tc.opts)
			if err != nil {
				t.Fatalf("Compare() error: %v", err)
			}
			s := rep.Summary
			if s.HasDrift != tc.want.hasDrift {
				t.Errorf("HasDrift = %v, want %v", s.HasDrift, tc.want.hasDrift)
			}
			if s.TablesOnlyA != tc.want.tablesOnlyA {
				t.Errorf("TablesOnlyA = %d, want %d", s.TablesOnlyA, tc.want.tablesOnlyA)
			}
			if s.TablesOnlyB != tc.want.tablesOnlyB {
				t.Errorf("TablesOnlyB = %d, want %d", s.TablesOnlyB, tc.want.tablesOnlyB)
			}
			if s.TablesModified != tc.want.tablesModified {
				t.Errorf("TablesModified = %d, want %d", s.TablesModified, tc.want.tablesModified)
			}
			if s.TablesIdentical != tc.want.tablesIdentical {
				t.Errorf("TablesIdentical = %d, want %d", s.TablesIdentical, tc.want.tablesIdentical)
			}
			if s.ColumnsOnlyA != tc.want.colsOnlyA {
				t.Errorf("ColumnsOnlyA = %d, want %d", s.ColumnsOnlyA, tc.want.colsOnlyA)
			}
			if s.ColumnsOnlyB != tc.want.colsOnlyB {
				t.Errorf("ColumnsOnlyB = %d, want %d", s.ColumnsOnlyB, tc.want.colsOnlyB)
			}
			if s.ColumnsModified != tc.want.colsModified {
				t.Errorf("ColumnsModified = %d, want %d", s.ColumnsModified, tc.want.colsModified)
			}
			if s.TypeMismatches != tc.want.typeDiffs {
				t.Errorf("TypeMismatches = %d, want %d", s.TypeMismatches, tc.want.typeDiffs)
			}
			if s.LengthMismatches != tc.want.lenDiffs {
				t.Errorf("LengthMismatches = %d, want %d", s.LengthMismatches, tc.want.lenDiffs)
			}
			if s.PrecisionMismatches != tc.want.precDiffs {
				t.Errorf("PrecisionMismatches = %d, want %d", s.PrecisionMismatches, tc.want.precDiffs)
			}
			if s.NullableMismatches != tc.want.nullDiffs {
				t.Errorf("NullableMismatches = %d, want %d", s.NullableMismatches, tc.want.nullDiffs)
			}
			if s.DefaultMismatches != tc.want.defaultDiffs {
				t.Errorf("DefaultMismatches = %d, want %d", s.DefaultMismatches, tc.want.defaultDiffs)
			}
			if s.UnmappedCount < tc.want.unmappedMin {
				t.Errorf("UnmappedCount = %d, want >= %d", s.UnmappedCount, tc.want.unmappedMin)
			}
			if s.CaseDiffCount < tc.want.caseDiffMin {
				t.Errorf("CaseDiffCount = %d, want >= %d", s.CaseDiffCount, tc.want.caseDiffMin)
			}
			if s.TablesIgnoredA != tc.want.ignoredA {
				t.Errorf("TablesIgnoredA = %d, want %d", s.TablesIgnoredA, tc.want.ignoredA)
			}
			if s.TablesIgnoredB != tc.want.ignoredB {
				t.Errorf("TablesIgnoredB = %d, want %d", s.TablesIgnoredB, tc.want.ignoredB)
			}
		})
	}
}

func BenchmarkDiff500Tables5000Columns(b *testing.B) {
	buildSnap := func(engine model.Engine, driftOffset int) model.SchemaSnapshot {
		tables := make([]model.Table, 0, 500)
		for t := 0; t < 500; t++ {
			cols := make([]model.Column, 0, 10)
			for c := 0; c < 10; c++ {
				lenVal := 100
				if driftOffset > 0 && t%20 == 0 && c == 2 {
					lenVal = 150
				}
				cols = append(cols, normalizer.NormalizeColumn(engine, normalizer.RawColumn{
					Name:          fmt.Sprintf("col_%02d", c),
					Ordinal:       c + 1,
					DataType:      "varchar",
					CharMaxLength: &lenVal,
					Nullable:      c%2 == 0,
				}))
			}
			tables = append(tables, model.Table{
				Schema:     "public",
				LogicalKey: fmt.Sprintf("table_%03d", t+driftOffset),
				Name:       fmt.Sprintf("table_%03d", t+driftOffset),
				Columns:    cols,
			})
		}
		return model.SchemaSnapshot{
			Engine: engine,
			Label:  string(engine),
			Tables: tables,
		}
	}

	snapA := buildSnap(model.EngineMSSQL, 0)
	snapB := buildSnap(model.EnginePostgres, 2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rep, err := Compare(snapA, snapB, Options{})
		if err != nil {
			b.Fatalf("Compare error: %v", err)
		}
		if !rep.Summary.HasDrift {
			b.Fatal("expected drift")
		}
	}
}
