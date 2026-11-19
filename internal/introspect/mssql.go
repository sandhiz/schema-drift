package introspect

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
	"schema-drift/internal/model"
	"schema-drift/internal/normalizer"
)

const (
	mssqlTablesQuery = `SELECT s.name AS table_schema, t.name AS table_name
FROM sys.tables t
INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
WHERE t.is_ms_shipped = 0
  AND (@p1 = '' OR s.name = @p1)
ORDER BY s.name, t.name`

	mssqlColumnsQuery = `SELECT s.name AS table_schema,
       t.name AS table_name,
       c.name AS column_name,
       c.column_id AS ordinal_position,
       ty.name AS data_type,
       CAST(c.max_length AS INT) AS max_length,
       CAST(c.precision AS INT) AS numeric_precision,
       CAST(c.scale AS INT) AS numeric_scale,
       c.is_nullable,
       CASE WHEN dc.object_id IS NOT NULL THEN 1 ELSE 0 END AS has_default,
       COALESCE(dc.definition, '') AS default_expr
FROM sys.columns c
INNER JOIN sys.tables t ON c.object_id = t.object_id
INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
INNER JOIN sys.types ty ON c.user_type_id = ty.user_type_id
LEFT JOIN sys.default_constraints dc ON c.default_object_id = dc.object_id
WHERE t.is_ms_shipped = 0
  AND (@p1 = '' OR s.name = @p1)
ORDER BY s.name, t.name, c.column_id`
)

type mssqlIntrospector struct {
	db   *sql.DB
	opts Options
}

func (m *mssqlIntrospector) Tables(ctx context.Context) ([]model.Table, error) {
	targetSchema := strings.TrimSpace(m.opts.Schema)

	observeQuery(m.opts, model.EngineMSSQL, mssqlTablesQuery)
	tableRows, err := m.db.QueryContext(ctx, mssqlTablesQuery, targetSchema)
	if err != nil {
		return nil, fmt.Errorf("mssql query tables: %w", err)
	}
	defer tableRows.Close()

	var tableOrder []tableKey
	knownTables := make(map[tableKey]struct{})
	for tableRows.Next() {
		var tk tableKey
		if err := tableRows.Scan(&tk.schema, &tk.name); err != nil {
			return nil, fmt.Errorf("mssql scan table: %w", err)
		}
		if _, exists := knownTables[tk]; !exists {
			knownTables[tk] = struct{}{}
			tableOrder = append(tableOrder, tk)
		}
	}
	if err := tableRows.Err(); err != nil {
		return nil, fmt.Errorf("mssql iterate tables: %w", err)
	}

	observeQuery(m.opts, model.EngineMSSQL, mssqlColumnsQuery)
	colRows, err := m.db.QueryContext(ctx, mssqlColumnsQuery, targetSchema)
	if err != nil {
		return nil, fmt.Errorf("mssql query columns: %w", err)
	}
	defer colRows.Close()

	colsByTable := make(map[tableKey][]model.Column, len(tableOrder))
	for colRows.Next() {
		var (
			schemaName, tableName, colName, dataType, defaultExpr string
			ordinal                                               int
			maxBytes, numPrec, numScale                           sql.NullInt64
			isNullable, hasDefault                                bool
		)
		if err := colRows.Scan(
			&schemaName, &tableName, &colName, &ordinal,
			&dataType, &maxBytes, &numPrec, &numScale,
			&isNullable, &hasDefault, &defaultExpr,
		); err != nil {
			return nil, fmt.Errorf("mssql scan column: %w", err)
		}

		tk := tableKey{schema: schemaName, name: tableName}
		if _, isBaseTable := knownTables[tk]; !isBaseTable {
			continue
		}

		col := normalizer.NormalizeColumn(model.EngineMSSQL, normalizer.RawColumn{
			Name:             colName,
			Ordinal:          ordinal,
			DataType:         dataType,
			MaxBytes:         intPtrFromNull(maxBytes),
			NumericPrecision: intPtrFromNull(numPrec),
			NumericScale:     intPtrFromNull(numScale),
			Nullable:         isNullable,
			HasDefault:       hasDefault,
			DefaultExpr:      defaultExpr,
		})
		colsByTable[tk] = append(colsByTable[tk], col)
	}
	if err := colRows.Err(); err != nil {
		return nil, fmt.Errorf("mssql iterate columns: %w", err)
	}

	return buildOrderedTables(model.EngineMSSQL, "dbo", tableOrder, colsByTable), nil
}
