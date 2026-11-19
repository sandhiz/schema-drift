package introspect

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"schema-drift/internal/model"
	"schema-drift/internal/normalizer"
)

const (
	pgCurrentSchemaQuery = `SELECT COALESCE(current_schema(), 'public')`

	pgTablesQuery = `SELECT table_schema, table_name
FROM information_schema.tables
WHERE table_type = 'BASE TABLE'
  AND table_schema NOT IN ('pg_catalog', 'information_schema')
  AND ($1 = '' OR table_schema = $1)
ORDER BY table_schema, table_name`

	pgColumnsQuery = `SELECT table_schema,
       table_name,
       column_name,
       ordinal_position,
       data_type,
       udt_name,
       character_maximum_length,
       numeric_precision,
       numeric_scale,
       is_nullable,
       column_default
FROM information_schema.columns
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
  AND ($1 = '' OR table_schema = $1)
ORDER BY table_schema, table_name, ordinal_position`
)

type postgresIntrospector struct {
	db   *sql.DB
	opts Options
}

func (p *postgresIntrospector) Tables(ctx context.Context) ([]model.Table, error) {
	defaultSchema := "public"
	observeQuery(p.opts, model.EnginePostgres, pgCurrentSchemaQuery)
	if err := p.db.QueryRowContext(ctx, pgCurrentSchemaQuery).Scan(&defaultSchema); err != nil {
		return nil, fmt.Errorf("postgres read current_schema: %w", err)
	}
	if strings.TrimSpace(defaultSchema) == "" {
		defaultSchema = "public"
	}

	targetSchema := strings.TrimSpace(p.opts.Schema)

	observeQuery(p.opts, model.EnginePostgres, pgTablesQuery)
	tableRows, err := p.db.QueryContext(ctx, pgTablesQuery, targetSchema)
	if err != nil {
		return nil, fmt.Errorf("postgres query tables: %w", err)
	}
	defer tableRows.Close()

	var tableOrder []tableKey
	knownTables := make(map[tableKey]struct{})
	for tableRows.Next() {
		var tk tableKey
		if err := tableRows.Scan(&tk.schema, &tk.name); err != nil {
			return nil, fmt.Errorf("postgres scan table: %w", err)
		}
		if _, exists := knownTables[tk]; !exists {
			knownTables[tk] = struct{}{}
			tableOrder = append(tableOrder, tk)
		}
	}
	if err := tableRows.Err(); err != nil {
		return nil, fmt.Errorf("postgres iterate tables: %w", err)
	}

	observeQuery(p.opts, model.EnginePostgres, pgColumnsQuery)
	colRows, err := p.db.QueryContext(ctx, pgColumnsQuery, targetSchema)
	if err != nil {
		return nil, fmt.Errorf("postgres query columns: %w", err)
	}
	defer colRows.Close()

	colsByTable := make(map[tableKey][]model.Column, len(tableOrder))
	for colRows.Next() {
		var (
			schemaName, tableName, colName, dataType, udtName, isNullable string
			ordinal                                                   int
			charMaxLen, numPrec, numScale                             sql.NullInt64
			colDefault                                                sql.NullString
		)
		if err := colRows.Scan(
			&schemaName, &tableName, &colName, &ordinal,
			&dataType, &udtName, &charMaxLen, &numPrec, &numScale,
			&isNullable, &colDefault,
		); err != nil {
			return nil, fmt.Errorf("postgres scan column: %w", err)
		}

		tk := tableKey{schema: schemaName, name: tableName}
		if _, isBaseTable := knownTables[tk]; !isBaseTable {
			// Exclude views and materialized views returned by information_schema.columns.
			continue
		}

		col := normalizer.NormalizeColumn(model.EnginePostgres, normalizer.RawColumn{
			Name:             colName,
			Ordinal:          ordinal,
			DataType:         dataType,
			ColumnType:       udtName,
			CharMaxLength:    intPtrFromNull(charMaxLen),
			NumericPrecision: intPtrFromNull(numPrec),
			NumericScale:     intPtrFromNull(numScale),
			Nullable:         strings.EqualFold(strings.TrimSpace(isNullable), "YES"),
			HasDefault:       colDefault.Valid,
			DefaultExpr:      colDefault.String,
		})
		colsByTable[tk] = append(colsByTable[tk], col)
	}
	if err := colRows.Err(); err != nil {
		return nil, fmt.Errorf("postgres iterate columns: %w", err)
	}

	return buildOrderedTables(model.EnginePostgres, defaultSchema, tableOrder, colsByTable), nil
}
