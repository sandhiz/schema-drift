package introspect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"schema-drift/internal/model"
	"schema-drift/internal/normalizer"
)

const (
	mysqlCurrentDBQuery = `SELECT COALESCE(DATABASE(), '')`

	mysqlTablesQuery = `SELECT TABLE_SCHEMA, TABLE_NAME
FROM information_schema.tables
WHERE TABLE_TYPE = 'BASE TABLE'
  AND TABLE_SCHEMA = ?
ORDER BY TABLE_SCHEMA, TABLE_NAME`

	mysqlColumnsQuery = `SELECT TABLE_SCHEMA,
       TABLE_NAME,
       COLUMN_NAME,
       ORDINAL_POSITION,
       DATA_TYPE,
       COLUMN_TYPE,
       CHARACTER_MAXIMUM_LENGTH,
       NUMERIC_PRECISION,
       NUMERIC_SCALE,
       IS_NULLABLE,
       COLUMN_DEFAULT
FROM information_schema.columns
WHERE TABLE_SCHEMA = ?
ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`
)

type mysqlIntrospector struct {
	db   *sql.DB
	opts Options
}

func (m *mysqlIntrospector) Tables(ctx context.Context) ([]model.Table, error) {
	observeQuery(m.opts, model.EngineMySQL, mysqlCurrentDBQuery)
	var currentDB string
	if err := m.db.QueryRowContext(ctx, mysqlCurrentDBQuery).Scan(&currentDB); err != nil {
		return nil, fmt.Errorf("mysql read current database: %w", err)
	}

	targetSchema := strings.TrimSpace(m.opts.Schema)
	if targetSchema == "" {
		targetSchema = strings.TrimSpace(currentDB)
	}
	if targetSchema == "" {
		return nil, errors.New("mysql DSN does not specify a database and --schema was not provided")
	}

	observeQuery(m.opts, model.EngineMySQL, mysqlTablesQuery)
	tableRows, err := m.db.QueryContext(ctx, mysqlTablesQuery, targetSchema)
	if err != nil {
		return nil, fmt.Errorf("mysql query tables: %w", err)
	}
	defer tableRows.Close()

	var tableOrder []tableKey
	knownTables := make(map[tableKey]struct{})
	for tableRows.Next() {
		var tk tableKey
		if err := tableRows.Scan(&tk.schema, &tk.name); err != nil {
			return nil, fmt.Errorf("mysql scan table: %w", err)
		}
		if _, exists := knownTables[tk]; !exists {
			knownTables[tk] = struct{}{}
			tableOrder = append(tableOrder, tk)
		}
	}
	if err := tableRows.Err(); err != nil {
		return nil, fmt.Errorf("mysql iterate tables: %w", err)
	}

	observeQuery(m.opts, model.EngineMySQL, mysqlColumnsQuery)
	colRows, err := m.db.QueryContext(ctx, mysqlColumnsQuery, targetSchema)
	if err != nil {
		return nil, fmt.Errorf("mysql query columns: %w", err)
	}
	defer colRows.Close()

	colsByTable := make(map[tableKey][]model.Column, len(tableOrder))
	for colRows.Next() {
		var (
			schemaName, tableName, colName, dataType, colType, isNullable string
			ordinal                                                     int
			charMaxLen, numPrec, numScale                               sql.NullInt64
			colDefault                                                  sql.NullString
		)
		if err := colRows.Scan(
			&schemaName, &tableName, &colName, &ordinal,
			&dataType, &colType, &charMaxLen, &numPrec, &numScale,
			&isNullable, &colDefault,
		); err != nil {
			return nil, fmt.Errorf("mysql scan column: %w", err)
		}

		tk := tableKey{schema: schemaName, name: tableName}
		if _, isBaseTable := knownTables[tk]; !isBaseTable {
			continue
		}

		col := normalizer.NormalizeColumn(model.EngineMySQL, normalizer.RawColumn{
			Name:             colName,
			Ordinal:          ordinal,
			DataType:         dataType,
			ColumnType:       colType,
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
		return nil, fmt.Errorf("mysql iterate columns: %w", err)
	}

	return buildOrderedTables(model.EngineMySQL, targetSchema, tableOrder, colsByTable), nil
}
