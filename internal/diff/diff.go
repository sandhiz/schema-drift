package diff

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"schema-drift/internal/model"
)

// DiffStatus classifies the comparison state of a table or column.
type DiffStatus string

const (
	StatusIdentical DiffStatus = "identical"
	StatusOnlyA     DiffStatus = "only_a"
	StatusOnlyB     DiffStatus = "only_b"
	StatusModified  DiffStatus = "modified"
)

// Options controls diff matching and ignore rules.
type Options struct {
	IgnorePatterns []string
	StrictCase     bool
}

// AttrDiff describes a single differing attribute on a matched column.
type AttrDiff struct {
	Field  string // "type", "length", "precision", "scale", "nullable", "default", "case"
	ValueA string
	ValueB string
	Note   string
}

// ColumnDiff represents the comparison outcome for one column inside a matched or unmatched table.
type ColumnDiff struct {
	Key                       string
	NameA                     string
	NameB                     string
	Status                    DiffStatus
	ColA                      *model.Column
	ColB                      *model.Column
	Diffs                     []AttrDiff
	Unmapped                  bool
	CaseDiff                  bool
	CrossEngineDefaultSkipped bool
}

// DisplayName returns the canonical display name for the column.
func (c ColumnDiff) DisplayName() string {
	switch {
	case c.NameA != "" && c.NameB != "" && c.NameA != c.NameB:
		return c.NameA + " / " + c.NameB
	case c.NameA != "":
		return c.NameA
	default:
		return c.NameB
	}
}

// TableDiff represents the comparison outcome for one table.
type TableDiff struct {
	LogicalKey    string
	NameA         string
	NameB         string
	SchemaA       string
	SchemaB       string
	Status        DiffStatus
	CaseDiff      bool
	UnmappedCount int
	Columns       []ColumnDiff
	OnlyACols     int
	OnlyBCols     int
	ModifiedCols  int
	IdenticalCols int
}

// DisplayName returns the table identifier shown in reports.
func (t TableDiff) DisplayName() string {
	if t.LogicalKey != "" {
		return t.LogicalKey
	}
	if t.NameA != "" {
		return t.NameA
	}
	return t.NameB
}

// SnapshotMeta holds non-secret metadata about one compared database.
type SnapshotMeta struct {
	Engine        model.Engine
	Label         string
	MaskedDSN     string
	DefaultSchema string
}

// Summary aggregates top-level metrics for terminal, HTML, and CI reporting.
type Summary struct {
	TablesReadA         int
	TablesReadB         int
	TablesIgnoredA      int
	TablesIgnoredB      int
	TablesOnlyA         int
	TablesOnlyB         int
	TablesModified      int
	TablesIdentical     int
	ColumnsOnlyA        int
	ColumnsOnlyB        int
	ColumnsModified     int
	ColumnsIdentical    int
	TypeMismatches      int
	LengthMismatches    int
	PrecisionMismatches int
	NullableMismatches  int
	DefaultMismatches   int
	UnmappedCount       int
	CaseDiffCount       int
	CrossEngine         bool
	HasDrift            bool
}

// TotalTableDiffs returns the count of tables that differ between A and B.
func (s Summary) TotalTableDiffs() int {
	return s.TablesOnlyA + s.TablesOnlyB + s.TablesModified
}

// TotalColumnDiffs returns the count of columns across modified tables that differ.
func (s Summary) TotalColumnDiffs() int {
	return s.ColumnsOnlyA + s.ColumnsOnlyB + s.ColumnsModified
}

// Report is the complete structural diff between two schema snapshots.
type Report struct {
	GeneratedAt     time.Time
	Duration        time.Duration
	SourceA         SnapshotMeta
	SourceB         SnapshotMeta
	IgnoredPatterns []string
	StrictCase      bool
	Summary         Summary
	Tables          []TableDiff
}

// TablesByStatus returns a filtered slice of tables matching the given status.
func (r Report) TablesByStatus(status DiffStatus) []TableDiff {
	var out []TableDiff
	for _, t := range r.Tables {
		if t.Status == status {
			out = append(out, t)
		}
	}
	return out
}

// Compare computes the structural diff between schema snapshot A and schema snapshot B.
func Compare(a, b model.SchemaSnapshot, opts Options) (Report, error) {
	matcher, err := NewIgnoreMatcher(opts.IgnorePatterns)
	if err != nil {
		return Report{}, err
	}

	crossEngine := a.Engine != "" && b.Engine != "" && a.Engine != b.Engine

	report := Report{
		GeneratedAt: time.Now().UTC(),
		SourceA: SnapshotMeta{
			Engine:        a.Engine,
			Label:         fallbackLabel(a.Label, a.MaskedDSN, string(a.Engine)+" A"),
			MaskedDSN:     a.MaskedDSN,
			DefaultSchema: a.DefaultSchema,
		},
		SourceB: SnapshotMeta{
			Engine:        b.Engine,
			Label:         fallbackLabel(b.Label, b.MaskedDSN, string(b.Engine)+" B"),
			MaskedDSN:     b.MaskedDSN,
			DefaultSchema: b.DefaultSchema,
		},
		IgnoredPatterns: matcher.Patterns(),
		StrictCase:      opts.StrictCase,
		Summary: Summary{
			TablesReadA: len(a.Tables),
			TablesReadB: len(b.Tables),
			CrossEngine: crossEngine,
		},
	}

	tablesA, ignoredA := filterTables(a.Tables, matcher)
	tablesB, ignoredB := filterTables(b.Tables, matcher)
	report.Summary.TablesIgnoredA = ignoredA
	report.Summary.TablesIgnoredB = ignoredB

	keyA, dupA := indexTables(tablesA)
	keyB, dupB := indexTables(tablesB)

	allKeysSet := make(map[string]struct{}, len(keyA)+len(keyB))
	for k := range keyA {
		allKeysSet[k] = struct{}{}
	}
	for k := range keyB {
		allKeysSet[k] = struct{}{}
	}

	keys := make([]string, 0, len(allKeysSet))
	for k := range allKeysSet {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		li, lj := strings.ToLower(keys[i]), strings.ToLower(keys[j])
		if li != lj {
			return li < lj
		}
		return keys[i] < keys[j]
	})

	_ = dupA
	_ = dupB

	report.Tables = make([]TableDiff, 0, len(keys))
	for _, k := range keys {
		ta, okA := keyA[k]
		tb, okB := keyB[k]

		switch {
		case okA && !okB:
			td := buildSingleSideTableDiff(ta, StatusOnlyA)
			report.Tables = append(report.Tables, td)
			report.Summary.TablesOnlyA++
			report.Summary.UnmappedCount += td.UnmappedCount

		case !okA && okB:
			td := buildSingleSideTableDiff(tb, StatusOnlyB)
			report.Tables = append(report.Tables, td)
			report.Summary.TablesOnlyB++
			report.Summary.UnmappedCount += td.UnmappedCount

		default:
			td := compareMatchedTables(ta, tb, crossEngine, opts.StrictCase, &report.Summary)
			report.Tables = append(report.Tables, td)
		}
	}

	report.Summary.HasDrift = report.Summary.TablesOnlyA > 0 ||
		report.Summary.TablesOnlyB > 0 ||
		report.Summary.TablesModified > 0 ||
		(opts.StrictCase && report.Summary.CaseDiffCount > 0)

	return report, nil
}

func filterTables(tables []model.Table, matcher *IgnoreMatcher) ([]model.Table, int) {
	kept := make([]model.Table, 0, len(tables))
	ignored := 0
	for _, t := range tables {
		if matcher.Match(t) {
			ignored++
			continue
		}
		kept = append(kept, t)
	}
	return kept, ignored
}

// indexTables indexes tables by lowercased LogicalKey unless a snapshot contains
// case-colliding names, in which case colliding tables retain their exact LogicalKey.
func indexTables(tables []model.Table) (map[string]model.Table, map[string]bool) {
	lowerCounts := make(map[string]int, len(tables))
	for _, t := range tables {
		lowerCounts[strings.ToLower(tableMatchKey(t))]++
	}
	colliding := make(map[string]bool)
	for lk, cnt := range lowerCounts {
		if cnt > 1 {
			colliding[lk] = true
		}
	}

	idx := make(map[string]model.Table, len(tables))
	for _, t := range tables {
		raw := tableMatchKey(t)
		lk := strings.ToLower(raw)
		if colliding[lk] {
			idx[raw] = t
		} else {
			idx[lk] = t
		}
	}
	return idx, colliding
}

func tableMatchKey(t model.Table) string {
	if t.LogicalKey != "" {
		return t.LogicalKey
	}
	return t.Name
}

func buildSingleSideTableDiff(t model.Table, status DiffStatus) TableDiff {
	td := TableDiff{
		LogicalKey: tableMatchKey(t),
		Status:     status,
		Columns:    make([]ColumnDiff, 0, len(t.Columns)),
	}
	if status == StatusOnlyA {
		td.NameA = t.Name
		td.SchemaA = t.Schema
	} else {
		td.NameB = t.Name
		td.SchemaB = t.Schema
	}

	cols := sortedColumns(t.Columns)
	for i := range cols {
		c := cols[i]
		if c.Unmapped {
			td.UnmappedCount++
		}
		cd := ColumnDiff{
			Key:      strings.ToLower(c.Name),
			Status:   status,
			Unmapped: c.Unmapped,
		}
		if status == StatusOnlyA {
			cd.NameA = c.Name
			cd.ColA = &c
		} else {
			cd.NameB = c.Name
			cd.ColB = &c
		}
		td.Columns = append(td.Columns, cd)
	}
	return td
}

func compareMatchedTables(ta, tb model.Table, crossEngine, strictCase bool, summary *Summary) TableDiff {
	td := TableDiff{
		LogicalKey: tableMatchKey(ta),
		NameA:      ta.Name,
		NameB:      tb.Name,
		SchemaA:    ta.Schema,
		SchemaB:    tb.Schema,
	}

	if ta.Name != tb.Name {
		td.CaseDiff = true
		summary.CaseDiffCount++
	}

	colMapA := indexColumns(ta.Columns)
	colMapB := indexColumns(tb.Columns)

	type orderedCol struct {
		key     string
		ordinal int
	}
	seen := make(map[string]struct{}, len(colMapA)+len(colMapB))
	var ordered []orderedCol

	for _, c := range sortedColumns(ta.Columns) {
		k := colLookupKey(c, ta.Columns)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			ordered = append(ordered, orderedCol{key: k, ordinal: c.Ordinal})
		}
	}
	for _, c := range sortedColumns(tb.Columns) {
		k := colLookupKey(c, tb.Columns)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			ordered = append(ordered, orderedCol{key: k, ordinal: 100000 + c.Ordinal})
		}
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ordinal != ordered[j].ordinal {
			return ordered[i].ordinal < ordered[j].ordinal
		}
		return strings.ToLower(ordered[i].key) < strings.ToLower(ordered[j].key)
	})

	td.Columns = make([]ColumnDiff, 0, len(ordered))
	for _, oc := range ordered {
		ca, okA := colMapA[oc.key]
		cb, okB := colMapB[oc.key]

		switch {
		case okA && !okB:
			colCopy := ca
			cd := ColumnDiff{
				Key:      oc.key,
				NameA:    ca.Name,
				Status:   StatusOnlyA,
				ColA:     &colCopy,
				Unmapped: ca.Unmapped,
			}
			td.Columns = append(td.Columns, cd)
			td.OnlyACols++
			summary.ColumnsOnlyA++
			if ca.Unmapped {
				td.UnmappedCount++
				summary.UnmappedCount++
			}

		case !okA && okB:
			colCopy := cb
			cd := ColumnDiff{
				Key:      oc.key,
				NameB:    cb.Name,
				Status:   StatusOnlyB,
				ColB:     &colCopy,
				Unmapped: cb.Unmapped,
			}
			td.Columns = append(td.Columns, cd)
			td.OnlyBCols++
			summary.ColumnsOnlyB++
			if cb.Unmapped {
				td.UnmappedCount++
				summary.UnmappedCount++
			}

		default:
			cd := compareMatchedColumns(oc.key, ca, cb, crossEngine, strictCase, summary)
			td.Columns = append(td.Columns, cd)
			if cd.Unmapped {
				td.UnmappedCount++
				summary.UnmappedCount++
			}
			if cd.Status == StatusModified {
				td.ModifiedCols++
				summary.ColumnsModified++
			} else {
				td.IdenticalCols++
				summary.ColumnsIdentical++
			}
		}
	}

	if td.OnlyACols > 0 || td.OnlyBCols > 0 || td.ModifiedCols > 0 || (strictCase && td.CaseDiff) {
		td.Status = StatusModified
		summary.TablesModified++
	} else {
		td.Status = StatusIdentical
		summary.TablesIdentical++
	}
	return td
}

func compareMatchedColumns(key string, ca, cb model.Column, crossEngine, strictCase bool, summary *Summary) ColumnDiff {
	aCopy := ca
	bCopy := cb
	cd := ColumnDiff{
		Key:      key,
		NameA:    ca.Name,
		NameB:    cb.Name,
		ColA:     &aCopy,
		ColB:     &bCopy,
		Unmapped: ca.Unmapped || cb.Unmapped,
	}

	if ca.Name != cb.Name {
		cd.CaseDiff = true
		summary.CaseDiffCount++
		if strictCase {
			cd.Diffs = append(cd.Diffs, AttrDiff{
				Field:  "case",
				ValueA: ca.Name,
				ValueB: cb.Name,
			})
		}
	}

	// 1. Type comparison
	typeMatches := false
	switch {
	case ca.Unmapped && cb.Unmapped:
		typeMatches = strings.EqualFold(ca.BaseType, cb.BaseType)
	case ca.Unmapped || cb.Unmapped:
		typeMatches = false
	default:
		typeMatches = ca.CanonicalType == cb.CanonicalType
	}

	if !typeMatches {
		cd.Diffs = append(cd.Diffs, AttrDiff{
			Field:  "type",
			ValueA: ca.FormattedCanonical(),
			ValueB: cb.FormattedCanonical(),
		})
		summary.TypeMismatches++
	} else if !ca.Unmapped && !cb.Unmapped {
		// 2. Length comparison (when canonical types match, e.g. string(50) vs string(100))
		if !intPtrEqual(ca.Length, cb.Length) {
			cd.Diffs = append(cd.Diffs, AttrDiff{
				Field:  "length",
				ValueA: formatOptInt(ca.Length),
				ValueB: formatOptInt(cb.Length),
			})
			summary.LengthMismatches++
		}

		// 3. Precision & Scale comparison (decimal/numeric)
		precDiff := !intPtrEqual(ca.Precision, cb.Precision)
		scaleDiff := !intPtrEqual(ca.Scale, cb.Scale)
		if precDiff {
			cd.Diffs = append(cd.Diffs, AttrDiff{
				Field:  "precision",
				ValueA: formatOptInt(ca.Precision),
				ValueB: formatOptInt(cb.Precision),
			})
		}
		if scaleDiff {
			cd.Diffs = append(cd.Diffs, AttrDiff{
				Field:  "scale",
				ValueA: formatOptInt(ca.Scale),
				ValueB: formatOptInt(cb.Scale),
			})
		}
		if precDiff || scaleDiff {
			summary.PrecisionMismatches++
		}
	}

	// 4. Nullability comparison
	if ca.Nullable != cb.Nullable {
		cd.Diffs = append(cd.Diffs, AttrDiff{
			Field:  "nullable",
			ValueA: formatNullable(ca.Nullable),
			ValueB: formatNullable(cb.Nullable),
		})
		summary.NullableMismatches++
	}

	// 5. Default expression comparison
	if crossEngine {
		if ca.HasDefault != cb.HasDefault {
			cd.Diffs = append(cd.Diffs, AttrDiff{
				Field:  "default",
				ValueA: formatDefaultPresence(ca),
				ValueB: formatDefaultPresence(cb),
			})
			summary.DefaultMismatches++
		} else if ca.HasDefault && cb.HasDefault && ca.NormalizedDefault != cb.NormalizedDefault {
			// Per PRD risk mitigation: cross-engine default expression text differs (e.g. getdate() vs now()),
			// so only presence/absence is flagged as drift.
			cd.CrossEngineDefaultSkipped = true
		}
	} else {
		if ca.HasDefault != cb.HasDefault || (ca.HasDefault && ca.NormalizedDefault != cb.NormalizedDefault) {
			cd.Diffs = append(cd.Diffs, AttrDiff{
				Field:  "default",
				ValueA: formatDefaultExpr(ca),
				ValueB: formatDefaultExpr(cb),
			})
			summary.DefaultMismatches++
		}
	}

	if len(cd.Diffs) > 0 {
		cd.Status = StatusModified
	} else {
		cd.Status = StatusIdentical
	}
	return cd
}

func indexColumns(cols []model.Column) map[string]model.Column {
	lowerCounts := make(map[string]int, len(cols))
	for _, c := range cols {
		lowerCounts[strings.ToLower(c.Name)]++
	}
	out := make(map[string]model.Column, len(cols))
	for _, c := range cols {
		lk := strings.ToLower(c.Name)
		if lowerCounts[lk] > 1 {
			out[c.Name] = c
		} else {
			out[lk] = c
		}
	}
	return out
}

func colLookupKey(c model.Column, all []model.Column) string {
	lk := strings.ToLower(c.Name)
	cnt := 0
	for _, item := range all {
		if strings.ToLower(item.Name) == lk {
			cnt++
		}
	}
	if cnt > 1 {
		return c.Name
	}
	return lk
}

func sortedColumns(cols []model.Column) []model.Column {
	out := make([]model.Column, len(cols))
	copy(out, cols)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Ordinal != out[j].Ordinal {
			return out[i].Ordinal < out[j].Ordinal
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
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

func formatOptInt(v *int) string {
	if v == nil {
		return "none"
	}
	return fmt.Sprintf("%d", *v)
}

func formatNullable(nullable bool) string {
	if nullable {
		return "NULL"
	}
	return "NOT NULL"
}

func formatDefaultPresence(c model.Column) string {
	if !c.HasDefault {
		return "none"
	}
	if c.DefaultExpr != "" {
		return c.DefaultExpr
	}
	return "<default>"
}

func formatDefaultExpr(c model.Column) string {
	if !c.HasDefault {
		return "none"
	}
	if c.NormalizedDefault != "" {
		return c.NormalizedDefault
	}
	if c.DefaultExpr != "" {
		return c.DefaultExpr
	}
	return "<empty>"
}

func fallbackLabel(primary, secondary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	if strings.TrimSpace(secondary) != "" {
		return secondary
	}
	return fallback
}
