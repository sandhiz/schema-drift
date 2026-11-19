package model

import (
	"fmt"
	"strings"
)

// Engine identifies a supported relational database engine.
type Engine string

const (
	EnginePostgres Engine = "postgres"
	EngineMySQL    Engine = "mysql"
	EngineMSSQL    Engine = "mssql"
)

// DisplayName returns a human-friendly engine name for reports and terminal output.
func (e Engine) DisplayName() string {
	switch e {
	case EnginePostgres:
		return "PostgreSQL"
	case EngineMySQL:
		return "MySQL/MariaDB"
	case EngineMSSQL:
		return "SQL Server"
	default:
		if e == "" {
			return "Unknown"
		}
		return string(e)
	}
}

// Column represents a normalized column definition within a table.
type Column struct {
	Name              string
	Ordinal           int
	RawType           string // Native signature, e.g. "nvarchar(50)", "decimal(10,2)", "tinyint(1)"
	BaseType          string // Lowercased base type name without parameters
	CanonicalType     string // Normalized cross-engine type name, or "unmapped"
	Unmapped          bool
	Length            *int // Character/byte length when applicable (nil for unbounded/MAX)
	Precision         *int // Precision for decimal/numeric types
	Scale             *int // Scale for decimal/numeric types
	Nullable          bool
	HasDefault        bool
	DefaultExpr       string // Raw default expression returned by the catalog
	NormalizedDefault string // Trimmed/unwrapped default expression for same-engine comparison
}

// FormattedCanonical renders the canonical type along with normalized length or precision/scale.
func (c Column) FormattedCanonical() string {
	if c.Unmapped {
		base := strings.TrimSpace(c.RawType)
		if base == "" {
			base = c.BaseType
		}
		if base == "" {
			return "unmapped"
		}
		return fmt.Sprintf("unmapped(%s)", base)
	}
	switch {
	case c.Precision != nil && c.Scale != nil:
		return fmt.Sprintf("%s(%d,%d)", c.CanonicalType, *c.Precision, *c.Scale)
	case c.Precision != nil:
		return fmt.Sprintf("%s(%d)", c.CanonicalType, *c.Precision)
	case c.Length != nil:
		return fmt.Sprintf("%s(%d)", c.CanonicalType, *c.Length)
	default:
		return c.CanonicalType
	}
}

// Table represents a normalized table and its ordered columns.
type Table struct {
	Schema     string // Raw catalog schema name (e.g. "dbo", "public", "sales")
	LogicalKey string // Case-preserved logical identifier ("users" for default schema, "sales.orders" otherwise)
	Name       string // Original table name
	Columns    []Column
}

// SchemaSnapshot holds the normalized schema read from one database endpoint.
type SchemaSnapshot struct {
	Engine        Engine
	Label         string // Display label (user-provided or masked endpoint)
	MaskedDSN     string // Fully masked DSN safe for logs and HTML output
	DefaultSchema string
	Tables        []Table
}
