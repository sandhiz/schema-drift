package introspect

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"schema-drift/internal/model"
)

func TestIsMetadataOnlyQuery(t *testing.T) {
	valid := []string{
		pgCurrentSchemaQuery,
		pgTablesQuery,
		pgColumnsQuery,
		mysqlCurrentDBQuery,
		mysqlTablesQuery,
		mysqlColumnsQuery,
		mssqlTablesQuery,
		mssqlColumnsQuery,
	}
	for _, q := range valid {
		if !IsMetadataOnlyQuery(q) {
			t.Errorf("IsMetadataOnlyQuery() returned false for valid catalog query:\n%s", q)
		}
	}

	invalid := []string{
		"",
		"SELECT * FROM users",
		"SELECT id FROM dbo.orders",
		"SELECT * FROM information_schema.tables; DELETE FROM users",
		"DELETE FROM information_schema.tables",
		"ALTER TABLE users ADD COLUMN x INT",
		"SELECT table_name FROM information_schema.tables JOIN users ON 1=1",
		"SELECT * INTO tmp FROM information_schema.tables",
	}
	for _, q := range invalid {
		if IsMetadataOnlyQuery(q) {
			t.Errorf("IsMetadataOnlyQuery() returned true for unsafe/user-data query: %q", q)
		}
	}
}

func TestPostgresIntrospector(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	var observed []string
	intr, err := NewIntrospector(model.EnginePostgres, db, Options{
		Observer: func(engine model.Engine, q string) {
			observed = append(observed, q)
			if !IsMetadataOnlyQuery(q) {
				t.Errorf("non-metadata query observed for %s: %s", engine, q)
			}
		},
	})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}

	mock.ExpectQuery(pgCurrentSchemaQuery).
		WillReturnRows(sqlmock.NewRows([]string{"current_schema"}).AddRow("public"))

	mock.ExpectQuery(pgTablesQuery).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name"}).
			AddRow("public", "users").
			AddRow("billing", "invoices"))

	mock.ExpectQuery(pgColumnsQuery).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{
			"table_schema", "table_name", "column_name", "ordinal_position",
			"data_type", "udt_name", "character_maximum_length",
			"numeric_precision", "numeric_scale", "is_nullable", "column_default",
		}).
			AddRow("public", "users", "id", 1, "bigint", "int8", nil, nil, nil, "NO", nil).
			AddRow("public", "users", "email", 2, "character varying", "varchar", 120, nil, nil, "NO", nil).
			AddRow("public", "user_summary_view", "email", 1, "character varying", "varchar", 120, nil, nil, "YES", nil).
			AddRow("billing", "invoices", "total", 1, "numeric", "numeric", nil, 12, 2, "NO", "0.00"))

	tables, err := intr.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables() error: %v", err)
	}
	if len(observed) != 3 {
		t.Fatalf("expected 3 observed queries, got %d", len(observed))
	}
	if len(tables) != 2 {
		t.Fatalf("expected 2 base tables (excluding view), got %d", len(tables))
	}
	if tables[0].LogicalKey != "users" || len(tables[0].Columns) != 2 {
		t.Fatalf("unexpected first table: %+v", tables[0])
	}
	if tables[1].LogicalKey != "billing.invoices" || tables[1].Columns[0].FormattedCanonical() != "decimal(12,2)" {
		t.Fatalf("unexpected second table: %+v", tables[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestMySQLIntrospector(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	intr, err := NewIntrospector(model.EngineMySQL, db, Options{
		Observer: func(engine model.Engine, q string) {
			if !IsMetadataOnlyQuery(q) {
				t.Errorf("non-metadata query observed for %s: %s", engine, q)
			}
		},
	})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}

	mock.ExpectQuery(mysqlCurrentDBQuery).
		WillReturnRows(sqlmock.NewRows([]string{"db"}).AddRow("app_db"))

	mock.ExpectQuery(mysqlTablesQuery).
		WithArgs("app_db").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_SCHEMA", "TABLE_NAME"}).
			AddRow("app_db", "orders"))

	mock.ExpectQuery(mysqlColumnsQuery).
		WithArgs("app_db").
		WillReturnRows(sqlmock.NewRows([]string{
			"TABLE_SCHEMA", "TABLE_NAME", "COLUMN_NAME", "ORDINAL_POSITION",
			"DATA_TYPE", "COLUMN_TYPE", "CHARACTER_MAXIMUM_LENGTH",
			"NUMERIC_PRECISION", "NUMERIC_SCALE", "IS_NULLABLE", "COLUMN_DEFAULT",
		}).
			AddRow("app_db", "orders", "is_paid", 1, "tinyint", "tinyint(1)", nil, nil, nil, "NO", "0").
			AddRow("app_db", "orders", "status", 2, "enum", "enum('open','closed')", nil, nil, nil, "NO", "open"))

	tables, err := intr.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables() error: %v", err)
	}
	if len(tables) != 1 || len(tables[0].Columns) != 2 {
		t.Fatalf("unexpected tables: %+v", tables)
	}
	if tables[0].LogicalKey != "orders" {
		t.Errorf("LogicalKey = %q, want %q", tables[0].LogicalKey, "orders")
	}
	if tables[0].Columns[0].CanonicalType != "boolean" {
		t.Errorf("tinyint(1) canonical = %q, want boolean", tables[0].Columns[0].CanonicalType)
	}
	if !tables[0].Columns[1].Unmapped {
		t.Errorf("enum expected to be marked Unmapped")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestMSSQLIntrospector(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	intr, err := NewIntrospector(model.EngineMSSQL, db, Options{
		Observer: func(engine model.Engine, q string) {
			if !IsMetadataOnlyQuery(q) {
				t.Errorf("non-metadata query observed for %s: %s", engine, q)
			}
		},
	})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}

	mock.ExpectQuery(mssqlTablesQuery).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name"}).
			AddRow("dbo", "Users"))

	mock.ExpectQuery(mssqlColumnsQuery).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{
			"table_schema", "table_name", "column_name", "ordinal_position",
			"data_type", "max_length", "numeric_precision", "numeric_scale",
			"is_nullable", "has_default", "default_expr",
		}).
			AddRow("dbo", "Users", "Email", 1, "nvarchar", 100, 0, 0, false, false, "").
			AddRow("dbo", "Users", "Bio", 2, "nvarchar", -1, 0, 0, true, false, "").
			AddRow("dbo", "Users", "IsActive", 3, "bit", 1, 1, 0, false, true, "((1))"))

	tables, err := intr.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables() error: %v", err)
	}
	if len(tables) != 1 || len(tables[0].Columns) != 3 {
		t.Fatalf("unexpected tables: %+v", tables)
	}
	if tables[0].LogicalKey != "Users" {
		t.Errorf("LogicalKey = %q, want Users", tables[0].LogicalKey)
	}
	if got := tables[0].Columns[0].FormattedCanonical(); got != "string(50)" {
		t.Errorf("Email formatted canonical = %q, want string(50)", got)
	}
	if got := tables[0].Columns[1].CanonicalType; got != "text" {
		t.Errorf("Bio canonical = %q, want text", got)
	}
	if got := tables[0].Columns[2].NormalizedDefault; got != "1" {
		t.Errorf("IsActive normalized default = %q, want 1", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
