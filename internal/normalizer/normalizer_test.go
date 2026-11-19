package normalizer

import (
	"testing"

	"schema-drift/internal/model"
)

func TestSupportedCanonicalTypesCount(t *testing.T) {
	types := SupportedCanonicalTypes()
	if len(types) < 15 {
		t.Fatalf("SupportedCanonicalTypes() = %d, expected >= 15 canonical types", len(types))
	}
}

func TestNormalizeColumnCrossEngineEquivalence(t *testing.T) {
	tests := []struct {
		name          string
		engine        model.Engine
		raw           RawColumn
		wantCanonical string
		wantUnmapped  bool
		wantLen       *int
		wantPrec      *int
		wantScale     *int
		wantRawType   string
	}{
		// String pairs
		{
			name:          "mssql nvarchar byte length halved to chars",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "email", DataType: "nvarchar", MaxBytes: intPtr(100)},
			wantCanonical: "string",
			wantLen:       intPtr(50),
			wantRawType:   "nvarchar(50)",
		},
		{
			name:          "postgres character varying(50)",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "email", DataType: "character varying", CharMaxLength: intPtr(50)},
			wantCanonical: "string",
			wantLen:       intPtr(50),
			wantRawType:   "character varying(50)",
		},
		{
			name:          "mysql varchar(50)",
			engine:        model.EngineMySQL,
			raw:           RawColumn{Name: "email", DataType: "varchar", CharMaxLength: intPtr(50)},
			wantCanonical: "string",
			wantLen:       intPtr(50),
			wantRawType:   "varchar(50)",
		},
		// Text / unbounded strings
		{
			name:          "mssql nvarchar(max) maps to text",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "bio", DataType: "nvarchar", MaxBytes: intPtr(-1)},
			wantCanonical: "text",
			wantRawType:   "nvarchar(max)",
		},
		{
			name:          "postgres bare character varying maps to text",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "bio", DataType: "character varying"},
			wantCanonical: "text",
			wantRawType:   "character varying",
		},
		{
			name:          "mysql longtext maps to text",
			engine:        model.EngineMySQL,
			raw:           RawColumn{Name: "bio", DataType: "longtext"},
			wantCanonical: "text",
			wantRawType:   "longtext",
		},
		// Integers
		{
			name:          "mssql int ignores spurious catalog precision",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "age", DataType: "int", NumericPrecision: intPtr(10), NumericScale: intPtr(0)},
			wantCanonical: "integer",
			wantRawType:   "int",
		},
		{
			name:          "postgres int4 maps to integer",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "age", DataType: "int4"},
			wantCanonical: "integer",
			wantRawType:   "int4",
		},
		{
			name:          "mysql mediumint maps to integer",
			engine:        model.EngineMySQL,
			raw:           RawColumn{Name: "age", DataType: "mediumint"},
			wantCanonical: "integer",
			wantRawType:   "mediumint",
		},
		{
			name:          "postgres bigserial maps to bigint",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "id", DataType: "bigserial"},
			wantCanonical: "bigint",
			wantRawType:   "bigserial",
		},
		{
			name:          "mssql tinyint",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "flags", DataType: "tinyint"},
			wantCanonical: "tinyint",
			wantRawType:   "tinyint",
		},
		{
			name:          "mysql tinyint(4) stays tinyint",
			engine:        model.EngineMySQL,
			raw:           RawColumn{Name: "flags", DataType: "tinyint", ColumnType: "tinyint(4)"},
			wantCanonical: "tinyint",
			wantRawType:   "tinyint(4)",
		},
		// Booleans
		{
			name:          "mysql tinyint(1) becomes boolean",
			engine:        model.EngineMySQL,
			raw:           RawColumn{Name: "active", DataType: "tinyint", ColumnType: "tinyint(1)"},
			wantCanonical: "boolean",
			wantRawType:   "tinyint(1)",
		},
		{
			name:          "mysql bit(1) becomes boolean",
			engine:        model.EngineMySQL,
			raw:           RawColumn{Name: "active", DataType: "bit", ColumnType: "bit(1)"},
			wantCanonical: "boolean",
			wantRawType:   "bit(1)",
		},
		{
			name:          "mssql bit becomes boolean",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "active", DataType: "bit"},
			wantCanonical: "boolean",
			wantRawType:   "bit",
		},
		{
			name:          "postgres boolean",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "active", DataType: "boolean"},
			wantCanonical: "boolean",
			wantRawType:   "boolean",
		},
		// Decimals & floats
		{
			name:          "postgres numeric(12,2)",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "amount", DataType: "numeric", NumericPrecision: intPtr(12), NumericScale: intPtr(2)},
			wantCanonical: "decimal",
			wantPrec:      intPtr(12),
			wantScale:     intPtr(2),
			wantRawType:   "numeric(12,2)",
		},
		{
			name:          "mssql money defaults to decimal(19,4)",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "amount", DataType: "money"},
			wantCanonical: "decimal",
			wantPrec:      intPtr(19),
			wantScale:     intPtr(4),
			wantRawType:   "money(19,4)",
		},
		{
			name:          "mssql smallmoney defaults to decimal(10,4)",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "fee", DataType: "smallmoney"},
			wantCanonical: "decimal",
			wantPrec:      intPtr(10),
			wantScale:     intPtr(4),
			wantRawType:   "smallmoney(10,4)",
		},
		{
			name:          "mssql real vs postgres float4",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "ratio", DataType: "float4"},
			wantCanonical: "float32",
			wantRawType:   "float4",
		},
		{
			name:          "mysql double maps to float64",
			engine:        model.EngineMySQL,
			raw:           RawColumn{Name: "score", DataType: "double"},
			wantCanonical: "float64",
			wantRawType:   "double",
		},
		// Temporal types
		{
			name:          "mssql datetime2 maps to timestamp",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "created_at", DataType: "datetime2"},
			wantCanonical: "timestamp",
			wantRawType:   "datetime2",
		},
		{
			name:          "mssql datetimeoffset maps to timestamptz",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "updated_at", DataType: "datetimeoffset"},
			wantCanonical: "timestamptz",
			wantRawType:   "datetimeoffset",
		},
		{
			name:          "postgres timetz",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "cutoff", DataType: "time with time zone"},
			wantCanonical: "timetz",
			wantRawType:   "time with time zone",
		},
		// Binary & blob
		{
			name:          "mssql varbinary(256) keeps length",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "digest", DataType: "varbinary", MaxBytes: intPtr(256)},
			wantCanonical: "binary",
			wantLen:       intPtr(256),
			wantRawType:   "varbinary(256)",
		},
		{
			name:          "mssql varbinary(max) becomes blob",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "payload", DataType: "varbinary", MaxBytes: intPtr(-1)},
			wantCanonical: "blob",
			wantRawType:   "varbinary(max)",
		},
		{
			name:          "postgres bytea becomes blob",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "payload", DataType: "bytea"},
			wantCanonical: "blob",
			wantRawType:   "bytea",
		},
		// UUID, JSON, XML
		{
			name:          "mssql uniqueidentifier maps to uuid",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "ext_id", DataType: "uniqueidentifier"},
			wantCanonical: "uuid",
			wantRawType:   "uniqueidentifier",
		},
		{
			name:          "postgres jsonb maps to json",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "meta", DataType: "jsonb"},
			wantCanonical: "json",
			wantRawType:   "jsonb",
		},
		{
			name:          "mssql xml maps to xml",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "doc", DataType: "xml"},
			wantCanonical: "xml",
			wantRawType:   "xml",
		},
		// Unmapped types
		{
			name:          "mssql hierarchyid flagged unmapped",
			engine:        model.EngineMSSQL,
			raw:           RawColumn{Name: "node", DataType: "hierarchyid"},
			wantCanonical: "unmapped",
			wantUnmapped:  true,
			wantRawType:   "hierarchyid",
		},
		{
			name:          "postgres USER-DEFINED uses udt_name from ColumnType",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "status", DataType: "USER-DEFINED", ColumnType: "order_status"},
			wantCanonical: "unmapped",
			wantUnmapped:  true,
			wantRawType:   "order_status",
		},
		{
			name:          "postgres ARRAY uses udt_name from ColumnType",
			engine:        model.EnginePostgres,
			raw:           RawColumn{Name: "tags", DataType: "ARRAY", ColumnType: "_text"},
			wantCanonical: "unmapped",
			wantUnmapped:  true,
			wantRawType:   "text[]",
		},
		{
			name:          "mysql enum flagged unmapped with full column type",
			engine:        model.EngineMySQL,
			raw:           RawColumn{Name: "role", DataType: "enum", ColumnType: "enum('admin','user')"},
			wantCanonical: "unmapped",
			wantUnmapped:  true,
			wantRawType:   "enum('admin','user')",
		},
		{
			name:          "unknown engine returns unmapped without error",
			engine:        model.Engine("oracle"),
			raw:           RawColumn{Name: "col", DataType: "varchar2", CharMaxLength: intPtr(40)},
			wantCanonical: "unmapped",
			wantUnmapped:  true,
			wantRawType:   "varchar2(40)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			col := NormalizeColumn(tc.engine, tc.raw)
			if col.CanonicalType != tc.wantCanonical {
				t.Errorf("CanonicalType = %q, want %q", col.CanonicalType, tc.wantCanonical)
			}
			if col.Unmapped != tc.wantUnmapped {
				t.Errorf("Unmapped = %v, want %v", col.Unmapped, tc.wantUnmapped)
			}
			if !intPtrEqual(col.Length, tc.wantLen) {
				t.Errorf("Length = %v, want %v", ptrVal(col.Length), ptrVal(tc.wantLen))
			}
			if !intPtrEqual(col.Precision, tc.wantPrec) {
				t.Errorf("Precision = %v, want %v", ptrVal(col.Precision), ptrVal(tc.wantPrec))
			}
			if !intPtrEqual(col.Scale, tc.wantScale) {
				t.Errorf("Scale = %v, want %v", ptrVal(col.Scale), ptrVal(tc.wantScale))
			}
			if col.RawType != tc.wantRawType {
				t.Errorf("RawType = %q, want %q", col.RawType, tc.wantRawType)
			}
		})
	}
}

func TestNormalizeDefaultExpr(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  0 ", "0"},
		{"((0))", "0"},
		{"('pending')", "'pending'"},
		{"(getdate())", "getdate()"},
		{"(1) + (2)", "(1) + (2)"},
		{"('(not balanced)')", "'(not balanced)'"},
		{"('(a''b)')", "'(a''b)'"},
	}
	for _, tc := range tests {
		if got := NormalizeDefaultExpr(tc.in); got != tc.want {
			t.Errorf("NormalizeDefaultExpr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildLogicalTableKey(t *testing.T) {
	tests := []struct {
		engine        model.Engine
		schema        string
		defaultSchema string
		table         string
		want          string
	}{
		{model.EngineMSSQL, "dbo", "dbo", "Users", "Users"},
		{model.EngineMSSQL, "sales", "dbo", "Orders", "sales.Orders"},
		{model.EnginePostgres, "public", "public", "users", "users"},
		{model.EnginePostgres, "billing", "public", "invoices", "billing.invoices"},
		{model.EngineMySQL, "app_db", "app_db", "users", "users"},
		{model.EngineMySQL, "other_db", "app_db", "logs", "other_db.logs"},
		{model.EnginePostgres, "", "", "raw_tbl", "raw_tbl"},
	}
	for _, tc := range tests {
		if got := BuildLogicalTableKey(tc.engine, tc.schema, tc.defaultSchema, tc.table); got != tc.want {
			t.Errorf("BuildLogicalTableKey(%s, %q, %q, %q) = %q, want %q", tc.engine, tc.schema, tc.defaultSchema, tc.table, got, tc.want)
		}
	}
}

func intPtrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func ptrVal(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
