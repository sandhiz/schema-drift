package render

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"

	"schema-drift/internal/diff"
	"schema-drift/internal/model"
)

//go:embed templates/report.html
var templateFS embed.FS

var htmlTmpl = template.Must(template.New("report.html").ParseFS(templateFS, "templates/report.html"))

type htmlColumnVM struct {
	Name                      string
	NameA                     string
	NameB                     string
	Status                    string
	StatusLabel               string
	RawA                      string
	CanonicalA                string
	NullableA                 string
	DefaultA                  string
	HasA                      bool
	RawB                      string
	CanonicalB                string
	NullableB                 string
	DefaultB                  string
	HasB                      bool
	Diffs                     []diff.AttrDiff
	Unmapped                  bool
	CaseDiff                  bool
	CrossEngineDefaultSkipped bool
	IsIdentical               bool
}

type htmlTableVM struct {
	AnchorID      string
	Name          string
	NameA         string
	NameB         string
	SchemaA       string
	SchemaB       string
	Status        string
	StatusLabel   string
	DefaultOpen   bool
	CaseDiff      bool
	UnmappedCount int
	HasUnmapped   bool
	OnlyACols     int
	OnlyBCols     int
	ModifiedCols  int
	IdenticalCols int
	TotalCols     int
	SearchIndex   string
	Columns       []htmlColumnVM
}

type htmlReportVM struct {
	Report               diff.Report
	GeneratedAtFormatted string
	DurationFormatted    string
	ParityPercent        int
	PctOnlyA             float64
	PctOnlyB             float64
	PctModified          float64
	PctIdentical         float64
	TotalTablesVisible   int
	TotalIgnored         int
	MarkdownSummary      string
	Tables               []htmlTableVM
}

// HTML writes a self-contained single-file HTML diff report with zero external dependencies.
func HTML(w io.Writer, rep diff.Report) error {
	vm := buildHTMLViewModel(rep)
	return htmlTmpl.Execute(w, vm)
}

func buildHTMLViewModel(rep diff.Report) htmlReportVM {
	total := len(rep.Tables)
	parityPct := 100
	var pctA, pctB, pctMod, pctEq float64
	if total > 0 {
		pctA = float64(rep.Summary.TablesOnlyA) * 100 / float64(total)
		pctB = float64(rep.Summary.TablesOnlyB) * 100 / float64(total)
		pctMod = float64(rep.Summary.TablesModified) * 100 / float64(total)
		pctEq = float64(rep.Summary.TablesIdentical) * 100 / float64(total)
		parityPct = int((float64(rep.Summary.TablesIdentical) * 100 / float64(total)) + 0.5)
	}

	tables := make([]htmlTableVM, 0, len(rep.Tables))
	for idx, td := range rep.Tables {
		cols := make([]htmlColumnVM, 0, len(td.Columns))
		var searchTerms []string
		searchTerms = append(searchTerms, td.DisplayName(), td.NameA, td.NameB, td.SchemaA, td.SchemaB)

		for _, cd := range td.Columns {
			cvm := htmlColumnVM{
				Name:                      cd.DisplayName(),
				NameA:                     cd.NameA,
				NameB:                     cd.NameB,
				Status:                    string(cd.Status),
				StatusLabel:               statusLabel(cd.Status),
				Diffs:                     cd.Diffs,
				Unmapped:                  cd.Unmapped,
				CaseDiff:                  cd.CaseDiff,
				CrossEngineDefaultSkipped: cd.CrossEngineDefaultSkipped,
				IsIdentical:               cd.Status == diff.StatusIdentical,
			}
			searchTerms = append(searchTerms, cd.NameA, cd.NameB)
			if cd.ColA != nil {
				cvm.HasA = true
				cvm.RawA = cd.ColA.RawType
				cvm.CanonicalA = cd.ColA.FormattedCanonical()
				cvm.NullableA = formatNullLabel(cd.ColA.Nullable)
				cvm.DefaultA = formatDefaultDisplay(cd.ColA)
				searchTerms = append(searchTerms, cd.ColA.RawType, cd.ColA.CanonicalType)
			}
			if cd.ColB != nil {
				cvm.HasB = true
				cvm.RawB = cd.ColB.RawType
				cvm.CanonicalB = cd.ColB.FormattedCanonical()
				cvm.NullableB = formatNullLabel(cd.ColB.Nullable)
				cvm.DefaultB = formatDefaultDisplay(cd.ColB)
				searchTerms = append(searchTerms, cd.ColB.RawType, cd.ColB.CanonicalType)
			}
			cols = append(cols, cvm)
		}

		// Open drifted tables by default unless dataset is very large (keep top 60 open on huge schemas for crisp initial load).
		defaultOpen := td.Status != diff.StatusIdentical && (total <= 100 || idx < 60)

		tables = append(tables, htmlTableVM{
			AnchorID:      fmt.Sprintf("tbl-%d", idx),
			Name:          td.DisplayName(),
			NameA:         td.NameA,
			NameB:         td.NameB,
			SchemaA:       td.SchemaA,
			SchemaB:       td.SchemaB,
			Status:        string(td.Status),
			StatusLabel:   statusLabel(td.Status),
			DefaultOpen:   defaultOpen,
			CaseDiff:      td.CaseDiff,
			UnmappedCount: td.UnmappedCount,
			HasUnmapped:   td.UnmappedCount > 0,
			OnlyACols:     td.OnlyACols,
			OnlyBCols:     td.OnlyBCols,
			ModifiedCols:  td.ModifiedCols,
			IdenticalCols: td.IdenticalCols,
			TotalCols:     len(td.Columns),
			SearchIndex:   strings.ToLower(strings.Join(searchTerms, " ")),
			Columns:       cols,
		})
	}

	dur := rep.Duration.String()
	if rep.Duration > 0 {
		dur = rep.Duration.Round(1e6).String()
	}

	return htmlReportVM{
		Report:               rep,
		GeneratedAtFormatted: rep.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		DurationFormatted:    dur,
		ParityPercent:        parityPct,
		PctOnlyA:             pctA,
		PctOnlyB:             pctB,
		PctModified:          pctMod,
		PctIdentical:         pctEq,
		TotalTablesVisible:   total,
		TotalIgnored:         rep.Summary.TablesIgnoredA + rep.Summary.TablesIgnoredB,
		MarkdownSummary:      MarkdownSummary(rep),
		Tables:               tables,
	}
}

func statusLabel(s diff.DiffStatus) string {
	switch s {
	case diff.StatusOnlyA:
		return "Only in A"
	case diff.StatusOnlyB:
		return "Only in B"
	case diff.StatusModified:
		return "Modified"
	default:
		return "Identical"
	}
}

func formatNullLabel(nullable bool) string {
	if nullable {
		return "NULL"
	}
	return "NOT NULL"
}

func formatDefaultDisplay(c *model.Column) string {
	if c == nil || !c.HasDefault {
		return ""
	}
	if c.DefaultExpr != "" {
		return c.DefaultExpr
	}
	return "<default>"
}

// MarkdownSummary formats a concise Markdown table summarizing a schema diff report.
func MarkdownSummary(rep diff.Report) string {
	var b strings.Builder
	b.WriteString("## schema-drift Report\n\n")
	fmt.Fprintf(&b, "- **Source A (%s):** `%s` (%d tables read)\n", rep.SourceA.Engine.DisplayName(), rep.SourceA.MaskedDSN, rep.Summary.TablesReadA)
	fmt.Fprintf(&b, "- **Source B (%s):** `%s` (%d tables read)\n\n", rep.SourceB.Engine.DisplayName(), rep.SourceB.MaskedDSN, rep.Summary.TablesReadB)
	fmt.Fprintf(&b, "| Metric | Count |\n| --- | ---: |\n")
	fmt.Fprintf(&b, "| Only in A (Tables) | %d |\n", rep.Summary.TablesOnlyA)
	fmt.Fprintf(&b, "| Only in B (Tables) | %d |\n", rep.Summary.TablesOnlyB)
	fmt.Fprintf(&b, "| Modified Tables | %d |\n", rep.Summary.TablesModified)
	fmt.Fprintf(&b, "| Identical Tables | %d |\n", rep.Summary.TablesIdentical)
	fmt.Fprintf(&b, "| Column Diffs (in Modified Tables) | %d |\n", rep.Summary.TotalColumnDiffs())
	if rep.Summary.UnmappedCount > 0 {
		fmt.Fprintf(&b, "| Unmapped Types | %d |\n", rep.Summary.UnmappedCount)
	}
	return b.String()
}
