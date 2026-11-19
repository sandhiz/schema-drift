package normalizer

import (
	"strings"

	"schema-drift/internal/model"
)

type typeRule struct {
	canonical    string
	keepLength   bool
	keepDecimal  bool
	unboundedStr bool // true when unbounded varchar/nvarchar should become "text"
}

// typeMappings is the single source of truth for cross-engine type normalization.
// It covers 20+ canonical type families across SQL Server, PostgreSQL, and MySQL/MariaDB.
var typeMappings = map[model.Engine]map[string]typeRule{
	model.EngineMSSQL: {
		// Strings & text
		"varchar":  {canonical: "string", keepLength: true, unboundedStr: true},
		"nvarchar": {canonical: "string", keepLength: true, unboundedStr: true},
		"char":     {canonical: "string", keepLength: true},
		"nchar":    {canonical: "string", keepLength: true},
		"text":     {canonical: "text"},
		"ntext":    {canonical: "text"},

		// Integers
		"tinyint":  {canonical: "tinyint"},
		"smallint": {canonical: "smallint"},
		"int":      {canonical: "integer"},
		"integer":  {canonical: "integer"},
		"bigint":   {canonical: "bigint"},

		// Exact & floating-point numbers
		"decimal":    {canonical: "decimal", keepDecimal: true},
		"numeric":    {canonical: "decimal", keepDecimal: true},
		"money":      {canonical: "decimal", keepDecimal: true},
		"smallmoney": {canonical: "decimal", keepDecimal: true},
		"real":       {canonical: "float32"},
		"float":      {canonical: "float64"},

		// Boolean
		"bit": {canonical: "boolean"},

		// Date & time
		"date":           {canonical: "date"},
		"time":           {canonical: "time"},
		"datetime":       {canonical: "timestamp"},
		"datetime2":      {canonical: "timestamp"},
		"smalldatetime":  {canonical: "timestamp"},
		"datetimeoffset": {canonical: "timestamptz"},

		// Binary & blob
		"binary":    {canonical: "binary", keepLength: true},
		"varbinary": {canonical: "binary", keepLength: true},
		"image":     {canonical: "blob"},

		// Structured / identifiers
		"uniqueidentifier": {canonical: "uuid"},
		"xml":              {canonical: "xml"},
	},
	model.EnginePostgres: {
		// Strings & text
		"character varying": {canonical: "string", keepLength: true, unboundedStr: true},
		"varchar":           {canonical: "string", keepLength: true, unboundedStr: true},
		"character":         {canonical: "string", keepLength: true, unboundedStr: true},
		"char":              {canonical: "string", keepLength: true, unboundedStr: true},
		"bpchar":            {canonical: "string", keepLength: true, unboundedStr: true},
		"text":              {canonical: "text"},

		// Integers
		"smallint":  {canonical: "smallint"},
		"int2":      {canonical: "smallint"},
		"smallserial": {canonical: "smallint"},
		"integer":   {canonical: "integer"},
		"int":       {canonical: "integer"},
		"int4":      {canonical: "integer"},
		"serial":    {canonical: "integer"},
		"bigint":    {canonical: "bigint"},
		"int8":      {canonical: "bigint"},
		"bigserial": {canonical: "bigint"},

		// Exact & floating-point numbers
		"numeric":          {canonical: "decimal", keepDecimal: true},
		"decimal":          {canonical: "decimal", keepDecimal: true},
		"money":            {canonical: "decimal", keepDecimal: true},
		"real":             {canonical: "float32"},
		"float4":           {canonical: "float32"},
		"double precision": {canonical: "float64"},
		"float8":           {canonical: "float64"},

		// Boolean
		"boolean": {canonical: "boolean"},
		"bool":    {canonical: "boolean"},

		// Date & time
		"date":                        {canonical: "date"},
		"time":                        {canonical: "time"},
		"time without time zone":      {canonical: "time"},
		"time with time zone":         {canonical: "timetz"},
		"timetz":                      {canonical: "timetz"},
		"timestamp":                   {canonical: "timestamp"},
		"timestamp without time zone": {canonical: "timestamp"},
		"timestamp with time zone":    {canonical: "timestamptz"},
		"timestamptz":                 {canonical: "timestamptz"},

		// Binary & blob
		"bytea": {canonical: "blob"},

		// Structured / identifiers
		"uuid":  {canonical: "uuid"},
		"json":  {canonical: "json"},
		"jsonb": {canonical: "json"},
		"xml":   {canonical: "xml"},
	},
	model.EngineMySQL: {
		// Strings & text
		"varchar":    {canonical: "string", keepLength: true},
		"char":       {canonical: "string", keepLength: true},
		"text":       {canonical: "text"},
		"tinytext":   {canonical: "text"},
		"mediumtext": {canonical: "text"},
		"longtext":   {canonical: "text"},

		// Integers
		"tinyint":   {canonical: "tinyint"},
		"smallint":  {canonical: "smallint"},
		"mediumint": {canonical: "integer"},
		"int":       {canonical: "integer"},
		"integer":   {canonical: "integer"},
		"bigint":    {canonical: "bigint"},

		// Exact & floating-point numbers
		"decimal":          {canonical: "decimal", keepDecimal: true},
		"numeric":          {canonical: "decimal", keepDecimal: true},
		"float":            {canonical: "float32"},
		"double":           {canonical: "float64"},
		"double precision": {canonical: "float64"},

		// Boolean
		"boolean": {canonical: "boolean"},
		"bool":    {canonical: "boolean"},

		// Date & time
		"date":      {canonical: "date"},
		"time":      {canonical: "time"},
		"datetime":  {canonical: "timestamp"},
		"timestamp": {canonical: "timestamptz"},

		// Binary & blob
		"binary":     {canonical: "binary", keepLength: true},
		"varbinary":  {canonical: "binary", keepLength: true},
		"blob":       {canonical: "blob"},
		"tinyblob":   {canonical: "blob"},
		"mediumblob": {canonical: "blob"},
		"longblob":   {canonical: "blob"},

		// Structured / identifiers
		"uuid": {canonical: "uuid"},
		"json": {canonical: "json"},
	},
}

// SupportedCanonicalTypes returns all canonical type names defined in typeMappings.
func SupportedCanonicalTypes() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, engMap := range typeMappings {
		for _, rule := range engMap {
			if _, ok := seen[rule.canonical]; !ok {
				seen[rule.canonical] = struct{}{}
				out = append(out, rule.canonical)
			}
		}
	}
	// Ensure "text" and "blob" and "boolean" are always present even if checked directly.
	for _, extra := range []string{"text", "blob", "boolean"} {
		if _, ok := seen[extra]; !ok {
			out = append(out, extra)
		}
	}
	return out
}

func lookupTypeRule(engine model.Engine, baseType, columnType string) (typeRule, bool) {
	engMap, ok := typeMappings[engine]
	if !ok {
		return typeRule{}, false
	}

	base := strings.ToLower(strings.TrimSpace(baseType))
	colType := strings.ToLower(strings.TrimSpace(columnType))

	// MySQL / MariaDB convention: tinyint(1) and bit(1) represent booleans.
	if engine == model.EngineMySQL {
		if base == "tinyint" && strings.HasPrefix(colType, "tinyint(1)") {
			return typeRule{canonical: "boolean"}, true
		}
		if base == "bit" && (strings.HasPrefix(colType, "bit(1)") || colType == "bit") {
			return typeRule{canonical: "boolean"}, true
		}
	}

	rule, found := engMap[base]
	return rule, found
}
