package introspect

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"schema-drift/internal/dsn"
	"schema-drift/internal/model"
	"schema-drift/internal/normalizer"
)

// Introspector reads structural metadata from a single database without touching user data.
type Introspector interface {
	Tables(ctx context.Context) ([]model.Table, error)
}

// QueryObserver receives every SQL query executed by an introspector for auditing or testing.
type QueryObserver func(engine model.Engine, query string)

// Options configures database catalog introspection.
type Options struct {
	Schema   string
	Observer QueryObserver
}

// Open establishes a read-only metadata connection pool and returns the engine introspector.
func Open(ctx context.Context, parsed dsn.ParsedDSN, opts Options) (Introspector, io.Closer, error) {
	db, err := sql.Open(parsed.DriverName, parsed.DriverDSN)
	if err != nil {
		return nil, nil, dsn.SanitizeError(fmt.Errorf("open %s connection: %w", parsed.Engine, err), parsed.Secrets...)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, nil, dsn.SanitizeError(fmt.Errorf("connect to %s (%s): %w", parsed.Engine, parsed.DisplayLabel, err), parsed.Secrets...)
	}

	intr, err := NewIntrospector(parsed.Engine, db, opts)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return intr, db, nil
}

// NewIntrospector wraps an existing *sql.DB (such as a sqlmock handle) with an engine introspector.
func NewIntrospector(engine model.Engine, db *sql.DB, opts Options) (Introspector, error) {
	switch engine {
	case model.EnginePostgres:
		return &postgresIntrospector{db: db, opts: opts}, nil
	case model.EngineMySQL:
		return &mysqlIntrospector{db: db, opts: opts}, nil
	case model.EngineMSSQL:
		return &mssqlIntrospector{db: db, opts: opts}, nil
	default:
		return nil, fmt.Errorf("unsupported engine: %q", engine)
	}
}

var (
	reForbiddenSQL = regexp.MustCompile(`(?i)\b(insert|update|delete|alter|drop|create|truncate|exec|execute|merge|grant|revoke|into|call|copy)\b`)
	reTableRef     = regexp.MustCompile(`(?i)(?:\bfrom\b|\bjoin\b)\s+([a-zA-Z0-9_.\[\]"` + "`" + `]+)`)
)

// IsMetadataOnlyQuery verifies that a SQL statement is strictly a read-only SELECT
// referencing only catalog metadata (information_schema.* or sys.*).
func IsMetadataOnlyQuery(query string) bool {
	normalized := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";"))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, ";") {
		return false
	}
	lower := strings.ToLower(normalized)
	if !strings.HasPrefix(lower, "select ") && !strings.HasPrefix(lower, "select\n") && !strings.HasPrefix(lower, "select\t") {
		return false
	}
	if reForbiddenSQL.MatchString(normalized) {
		return false
	}

	matches := reTableRef.FindAllStringSubmatch(normalized, -1)
	if len(matches) == 0 {
		// Allow scalar metadata probes like "SELECT COALESCE(current_schema(), 'public')"
		// or "SELECT COALESCE(DATABASE(), '')".
		return strings.Contains(lower, "current_schema(") || strings.Contains(lower, "database(")
	}
	for _, m := range matches {
		ident := strings.ToLower(strings.NewReplacer("[", "", "]", "", `"`, "", "`", "").Replace(m[1]))
		if !strings.HasPrefix(ident, "information_schema.") && !strings.HasPrefix(ident, "sys.") {
			return false
		}
	}
	return true
}

func observeQuery(opts Options, engine model.Engine, query string) {
	if opts.Observer != nil {
		opts.Observer(engine, query)
	}
}

func buildOrderedTables(engine model.Engine, defaultSchema string, tableOrder []tableKey, colsByTable map[tableKey][]model.Column) []model.Table {
	out := make([]model.Table, 0, len(tableOrder))
	for _, tk := range tableOrder {
		cols := colsByTable[tk]
		out = append(out, model.Table{
			Schema:     tk.schema,
			LogicalKey: normalizer.BuildLogicalTableKey(engine, tk.schema, defaultSchema, tk.name),
			Name:       tk.name,
			Columns:    cols,
		})
	}
	return out
}

type tableKey struct {
	schema string
	name   string
}

func intPtrFromNull(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}
