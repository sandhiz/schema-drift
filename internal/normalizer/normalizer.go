package normalizer

import (
	"fmt"
	"strings"

	"schema-drift/internal/model"
)

// RawColumn carries unnormalized column metadata scanned from database catalogs.
type RawColumn struct {
	Name             string
	Ordinal          int
	DataType         string // Primary catalog type name (e.g., "character varying", "nvarchar", "tinyint")
	ColumnType       string // Full or subtype signature when available (e.g., "tinyint(1)", "enum('a','b')", udt_name)
	CharMaxLength    *int   // Character length from information_schema (Postgres/MySQL)
	MaxBytes         *int   // Byte length from SQL Server sys.columns.max_length (-1 means MAX)
	NumericPrecision *int
	NumericScale     *int
	Nullable         bool
	HasDefault       bool
	DefaultExpr      string
}

// NormalizeColumn maps engine-specific column metadata into the canonical schema model.
func NormalizeColumn(engine model.Engine, raw RawColumn) model.Column {
	baseType := strings.ToLower(strings.TrimSpace(raw.DataType))
	colTypeLower := strings.ToLower(strings.TrimSpace(raw.ColumnType))

	// PostgreSQL reports custom/extension/array types as "user-defined" or "array" in data_type;
	// fall back to udt_name stored in ColumnType so unmapped diagnostics stay clear.
	effectiveBase := baseType
	if engine == model.EnginePostgres && (baseType == "user-defined" || baseType == "array") && colTypeLower != "" {
		effectiveBase = strings.TrimPrefix(colTypeLower, "_")
		if baseType == "array" {
			effectiveBase += "[]"
		}
	}

	col := model.Column{
		Name:              raw.Name,
		Ordinal:           raw.Ordinal,
		BaseType:          effectiveBase,
		Nullable:          raw.Nullable,
		HasDefault:        raw.HasDefault,
		DefaultExpr:       strings.TrimSpace(raw.DefaultExpr),
		NormalizedDefault: NormalizeDefaultExpr(raw.DefaultExpr),
	}

	length, isMax := resolveLength(engine, effectiveBase, raw.CharMaxLength, raw.MaxBytes)
	rule, found := lookupTypeRule(engine, effectiveBase, raw.ColumnType)

	if !found {
		col.CanonicalType = "unmapped"
		col.Unmapped = true
		col.RawType = formatRawType(effectiveBase, raw.ColumnType, length, isMax, raw.NumericPrecision, raw.NumericScale)
		return col
	}

	canonical := rule.canonical
	var normLen, normPrec, normScale *int

	// Unbounded strings (SQL Server varchar(max)/nvarchar(max) or Postgres bare varchar) map to "text".
	if rule.unboundedStr && (isMax || length == nil) {
		canonical = "text"
	} else if engine == model.EngineMSSQL && effectiveBase == "varbinary" && isMax {
		// SQL Server varbinary(max) matches PostgreSQL bytea and MySQL blob.
		canonical = "blob"
	} else if rule.keepLength && length != nil {
		normLen = intPtr(*length)
	}

	if rule.keepDecimal {
		if raw.NumericPrecision != nil {
			normPrec = intPtr(*raw.NumericPrecision)
			if raw.NumericScale != nil {
				normScale = intPtr(*raw.NumericScale)
			}
		} else if engine == model.EngineMSSQL {
			switch effectiveBase {
			case "money":
				normPrec = intPtr(19)
				normScale = intPtr(4)
			case "smallmoney":
				normPrec = intPtr(10)
				normScale = intPtr(4)
			}
		}
	}

	col.CanonicalType = canonical
	col.Length = normLen
	col.Precision = normPrec
	col.Scale = normScale
	col.RawType = formatRawType(effectiveBase, raw.ColumnType, length, isMax, normPrec, normScale)
	return col
}

// BuildLogicalTableKey normalizes default schema prefixes across engines so
// dbo.users (SQL Server), public.users (PostgreSQL), and mydb.users (MySQL) match cleanly.
func BuildLogicalTableKey(engine model.Engine, schema, defaultSchema, tableName string) string {
	s := strings.TrimSpace(schema)
	t := strings.TrimSpace(tableName)
	if isDefaultSchema(engine, s, defaultSchema) {
		return t
	}
	return s + "." + t
}

func isDefaultSchema(engine model.Engine, schema, defaultSchema string) bool {
	if schema == "" {
		return true
	}
	if defaultSchema != "" && strings.EqualFold(schema, defaultSchema) {
		return true
	}
	switch engine {
	case model.EngineMSSQL:
		return strings.EqualFold(schema, "dbo")
	case model.EnginePostgres:
		return strings.EqualFold(schema, "public")
	case model.EngineMySQL:
		return defaultSchema == "" || strings.EqualFold(schema, defaultSchema)
	default:
		return false
	}
}

// NormalizeDefaultExpr trims whitespace and removes redundant balanced outer parentheses
// (e.g., SQL Server "((0))" -> "0", "('active')" -> "'active'").
func NormalizeDefaultExpr(expr string) string {
	s := strings.TrimSpace(expr)
	for len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		if !outerParensBalanced(s) {
			break
		}
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func outerParensBalanced(s string) bool {
	depth := 0
	inSingleQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' {
			// Handle escaped '' in SQL string literals.
			if inSingleQuote && i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			inSingleQuote = !inSingleQuote
			continue
		}
		if inSingleQuote {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i < len(s)-1 {
				return false
			}
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func resolveLength(engine model.Engine, baseType string, charMaxLen, maxBytes *int) (*int, bool) {
	if charMaxLen != nil {
		if *charMaxLen < 0 {
			return nil, true
		}
		return intPtr(*charMaxLen), false
	}
	if engine == model.EngineMSSQL && maxBytes != nil {
		if *maxBytes == -1 {
			return nil, true
		}
		switch baseType {
		case "nvarchar", "nchar":
			chars := *maxBytes / 2
			return &chars, false
		case "varchar", "char", "binary", "varbinary":
			return intPtr(*maxBytes), false
		}
	}
	return nil, false
}

func formatRawType(baseType, columnType string, length *int, isMax bool, prec, scale *int) string {
	colTrimmed := strings.TrimSpace(columnType)
	if colTrimmed != "" && (strings.Contains(colTrimmed, "(") || strings.HasPrefix(strings.ToLower(baseType), "enum") || strings.HasPrefix(strings.ToLower(baseType), "set")) {
		return colTrimmed
	}
	if isMax {
		return fmt.Sprintf("%s(max)", baseType)
	}
	if prec != nil && scale != nil {
		return fmt.Sprintf("%s(%d,%d)", baseType, *prec, *scale)
	}
	if prec != nil {
		return fmt.Sprintf("%s(%d)", baseType, *prec)
	}
	if length != nil {
		return fmt.Sprintf("%s(%d)", baseType, *length)
	}
	if baseType != "" {
		return baseType
	}
	return colTrimmed
}

func intPtr(v int) *int {
	return &v
}
