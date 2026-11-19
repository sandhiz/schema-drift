// Package demo provides realistic sample cross-engine schema snapshots
// (SQL Server legacy database -> PostgreSQL 16 target) for preview and UI exploration.
package demo

import (
	"schema-drift/internal/model"
	"schema-drift/internal/normalizer"
)

func intPtr(v int) *int { return &v }

func col(eng model.Engine, name string, ord int, dataType, colType string, charLen, maxBytes, prec, scale *int, nullable, hasDef bool, defExpr string) model.Column {
	return normalizer.NormalizeColumn(eng, normalizer.RawColumn{
		Name:             name,
		Ordinal:          ord,
		DataType:         dataType,
		ColumnType:       colType,
		CharMaxLength:    charLen,
		MaxBytes:         maxBytes,
		NumericPrecision: prec,
		NumericScale:     scale,
		Nullable:         nullable,
		HasDefault:       hasDef,
		DefaultExpr:      defExpr,
	})
}

// BuildSnapshots returns a realistic pair of SQL Server and PostgreSQL schemas
// showcasing missing tables, missing columns, type/length/precision/nullable/default diffs,
// unmapped types, casing notices, and glob-ignored tables.
func BuildSnapshots() (model.SchemaSnapshot, model.SchemaSnapshot) {
	mssql := model.EngineMSSQL
	pg := model.EnginePostgres

	snapA := model.SchemaSnapshot{
		Engine:        mssql,
		Label:         "sqlserver://legacy-erp.prod:1433/CoreBilling",
		MaskedDSN:     "sqlserver://readonly:***@legacy-erp.prod:1433?database=CoreBilling&ApplicationIntent=ReadOnly",
		DefaultSchema: "dbo",
		Tables: []model.Table{
			{
				Schema: "dbo", LogicalKey: "Users", Name: "Users",
				Columns: []model.Column{
					col(mssql, "UserID", 1, "bigint", "", nil, nil, nil, nil, false, false, ""),
					col(mssql, "email", 2, "nvarchar", "", nil, intPtr(240), nil, nil, false, false, ""),
					col(mssql, "full_name", 3, "nvarchar", "", nil, intPtr(160), nil, nil, false, false, ""),
					col(mssql, "is_active", 4, "bit", "", nil, nil, nil, nil, false, true, "((1))"),
					col(mssql, "created_at", 5, "datetimeoffset", "", nil, nil, nil, nil, false, true, "(sysdatetimeoffset())"),
					col(mssql, "legacy_pin", 6, "char", "", nil, intPtr(6), nil, nil, true, false, ""),
					col(mssql, "org_node", 7, "hierarchyid", "", nil, nil, nil, nil, true, false, ""),
				},
			},
			{
				Schema: "dbo", LogicalKey: "orders", Name: "orders",
				Columns: []model.Column{
					col(mssql, "id", 1, "uniqueidentifier", "", nil, nil, nil, nil, false, false, ""),
					col(mssql, "user_id", 2, "bigint", "", nil, nil, nil, nil, false, false, ""),
					col(mssql, "total_amount", 3, "decimal", "", nil, nil, intPtr(10), intPtr(2), false, true, "((0.00))"),
					col(mssql, "status", 4, "nvarchar", "", nil, intPtr(64), nil, nil, false, true, "('pending')"),
					col(mssql, "notes", 5, "nvarchar", "", nil, intPtr(-1), nil, nil, true, false, ""),
					col(mssql, "settled_at", 6, "datetime2", "", nil, nil, nil, nil, true, true, "(getdate())"),
				},
			},
			{
				Schema: "billing", LogicalKey: "billing.invoices", Name: "invoices",
				Columns: []model.Column{
					col(mssql, "invoice_id", 1, "uniqueidentifier", "", nil, nil, nil, nil, false, false, ""),
					col(mssql, "subtotal", 2, "money", "", nil, nil, nil, nil, false, false, ""),
					col(mssql, "tax_rate", 3, "decimal", "", nil, nil, intPtr(5), intPtr(4), false, true, "((0))"),
					col(mssql, "pdf_blob", 4, "varbinary", "", nil, intPtr(-1), nil, nil, true, false, ""),
				},
			},
			{
				Schema: "dbo", LogicalKey: "products", Name: "products",
				Columns: []model.Column{
					col(mssql, "id", 1, "int", "", nil, nil, nil, nil, false, false, ""),
					col(mssql, "sku", 2, "varchar", "", nil, intPtr(40), nil, nil, false, false, ""),
					col(mssql, "title", 3, "nvarchar", "", nil, intPtr(300), nil, nil, false, false, ""),
					col(mssql, "weight_kg", 4, "real", "", nil, nil, nil, nil, true, false, ""),
					col(mssql, "is_published", 5, "bit", "", nil, nil, nil, nil, false, true, "((0))"),
				},
			},
			{
				Schema: "dbo", LogicalKey: "countries", Name: "countries",
				Columns: []model.Column{
					col(mssql, "code", 1, "char", "", nil, intPtr(2), nil, nil, false, false, ""),
					col(mssql, "name", 2, "nvarchar", "", nil, intPtr(100), nil, nil, false, false, ""),
				},
			},
			{
				Schema: "dbo", LogicalKey: "currencies", Name: "currencies",
				Columns: []model.Column{
					col(mssql, "iso_code", 1, "char", "", nil, intPtr(3), nil, nil, false, false, ""),
					col(mssql, "symbol", 2, "nvarchar", "", nil, intPtr(16), nil, nil, false, false, ""),
					col(mssql, "minor_units", 3, "smallint", "", nil, nil, nil, nil, false, true, "((2))"),
				},
			},
			{
				Schema: "dbo", LogicalKey: "legacy_sessions", Name: "legacy_sessions",
				Columns: []model.Column{
					col(mssql, "session_token", 1, "varchar", "", nil, intPtr(64), nil, nil, false, false, ""),
					col(mssql, "user_id", 2, "bigint", "", nil, nil, nil, nil, false, false, ""),
					col(mssql, "expires_at", 3, "datetime2", "", nil, nil, nil, nil, false, false, ""),
				},
			},
			{
				Schema: "dbo", LogicalKey: "soap_xml_queue", Name: "soap_xml_queue",
				Columns: []model.Column{
					col(mssql, "id", 1, "bigint", "", nil, nil, nil, nil, false, false, ""),
					col(mssql, "envelope", 2, "xml", "", nil, nil, nil, nil, false, false, ""),
					col(mssql, "queued_at", 3, "datetimeoffset", "", nil, nil, nil, nil, false, true, "(sysdatetimeoffset())"),
				},
			},
			{
				Schema: "dbo", LogicalKey: "temp_etl_staging", Name: "temp_etl_staging",
				Columns: []model.Column{
					col(mssql, "row_id", 1, "bigint", "", nil, nil, nil, nil, false, false, ""),
				},
			},
			{
				Schema: "dbo", LogicalKey: "audit_2025_archive", Name: "audit_2025_archive",
				Columns: []model.Column{
					col(mssql, "event_id", 1, "bigint", "", nil, nil, nil, nil, false, false, ""),
				},
			},
		},
	}

	snapB := model.SchemaSnapshot{
		Engine:        pg,
		Label:         "postgres://aurora-v16.prod:5432/core_billing",
		MaskedDSN:     "postgres://readonly:***@aurora-v16.prod:5432/core_billing?sslmode=require&default_transaction_read_only=on",
		DefaultSchema: "public",
		Tables: []model.Table{
			{
				Schema: "public", LogicalKey: "users", Name: "users",
				Columns: []model.Column{
					col(pg, "userid", 1, "bigint", "int8", nil, nil, nil, nil, false, false, ""),
					col(pg, "email", 2, "character varying", "varchar", intPtr(120), nil, nil, nil, false, false, ""),
					col(pg, "full_name", 3, "character varying", "varchar", intPtr(120), nil, nil, nil, true, false, ""),
					col(pg, "is_active", 4, "boolean", "bool", nil, nil, nil, nil, false, true, "true"),
					col(pg, "created_at", 5, "timestamp with time zone", "timestamptz", nil, nil, nil, nil, false, true, "now()"),
					col(pg, "mfa_enabled", 6, "boolean", "bool", nil, nil, nil, nil, false, true, "false"),
					col(pg, "org_node", 7, "text", "text", nil, nil, nil, nil, true, false, ""),
				},
			},
			{
				Schema: "public", LogicalKey: "orders", Name: "orders",
				Columns: []model.Column{
					col(pg, "id", 1, "uuid", "uuid", nil, nil, nil, nil, false, false, ""),
					col(pg, "user_id", 2, "bigint", "int8", nil, nil, nil, nil, false, false, ""),
					col(pg, "total_amount", 3, "numeric", "numeric", nil, nil, intPtr(12), intPtr(2), false, true, "0.00"),
					col(pg, "status", 4, "character varying", "varchar", intPtr(32), nil, nil, nil, false, true, "'pending'::character varying"),
					col(pg, "notes", 5, "text", "text", nil, nil, nil, nil, true, false, ""),
					col(pg, "settled_at", 6, "timestamp without time zone", "timestamp", nil, nil, nil, nil, true, false, ""),
				},
			},
			{
				Schema: "billing", LogicalKey: "billing.invoices", Name: "invoices",
				Columns: []model.Column{
					col(pg, "invoice_id", 1, "uuid", "uuid", nil, nil, nil, nil, false, false, ""),
					col(pg, "subtotal", 2, "numeric", "numeric", nil, nil, intPtr(19), intPtr(4), false, false, ""),
					col(pg, "tax_rate", 3, "numeric", "numeric", nil, nil, intPtr(5), intPtr(4), false, true, "0"),
					col(pg, "pdf_blob", 4, "bytea", "bytea", nil, nil, nil, nil, true, false, ""),
				},
			},
			{
				Schema: "public", LogicalKey: "products", Name: "products",
				Columns: []model.Column{
					col(pg, "id", 1, "bigint", "int8", nil, nil, nil, nil, false, false, ""),
					col(pg, "sku", 2, "character varying", "varchar", intPtr(40), nil, nil, nil, false, false, ""),
					col(pg, "title", 3, "character varying", "varchar", intPtr(150), nil, nil, nil, false, false, ""),
					col(pg, "weight_kg", 4, "double precision", "float8", nil, nil, nil, nil, true, false, ""),
					col(pg, "is_published", 5, "boolean", "bool", nil, nil, nil, nil, false, true, "false"),
				},
			},
			{
				Schema: "public", LogicalKey: "countries", Name: "countries",
				Columns: []model.Column{
					col(pg, "code", 1, "character", "bpchar", intPtr(2), nil, nil, nil, false, false, ""),
					col(pg, "name", 2, "character varying", "varchar", intPtr(100), nil, nil, nil, false, false, ""),
				},
			},
			{
				Schema: "public", LogicalKey: "currencies", Name: "currencies",
				Columns: []model.Column{
					col(pg, "iso_code", 1, "character", "bpchar", intPtr(3), nil, nil, nil, false, false, ""),
					col(pg, "symbol", 2, "character varying", "varchar", intPtr(8), nil, nil, nil, false, false, ""),
					col(pg, "minor_units", 3, "smallint", "int2", nil, nil, nil, nil, false, true, "2"),
				},
			},
			{
				Schema: "public", LogicalKey: "webhook_events", Name: "webhook_events",
				Columns: []model.Column{
					col(pg, "id", 1, "bigint", "int8", nil, nil, nil, nil, false, false, ""),
					col(pg, "payload", 2, "jsonb", "jsonb", nil, nil, nil, nil, false, false, ""),
					col(pg, "search_vec", 3, "tsvector", "tsvector", nil, nil, nil, nil, true, false, ""),
					col(pg, "received_at", 4, "timestamp with time zone", "timestamptz", nil, nil, nil, nil, false, true, "now()"),
				},
			},
			{
				Schema: "public", LogicalKey: "idempotency_keys", Name: "idempotency_keys",
				Columns: []model.Column{
					col(pg, "key", 1, "character varying", "varchar", intPtr(128), nil, nil, nil, false, false, ""),
					col(pg, "expires_at", 2, "timestamp with time zone", "timestamptz", nil, nil, nil, nil, false, false, ""),
				},
			},
			{
				Schema: "public", LogicalKey: "temp_pg_import", Name: "temp_pg_import",
				Columns: []model.Column{
					col(pg, "row_id", 1, "bigint", "int8", nil, nil, nil, nil, false, false, ""),
				},
			},
		},
	}

	return snapA, snapB
}
