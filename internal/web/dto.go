package web

import (
	"fmt"
	"time"

	"schema-drift/internal/diff"
	"schema-drift/internal/model"
	"schema-drift/internal/render"
)

// TestConnectionResponse summarizes a read-only metadata probe against one endpoint.
type TestConnectionResponse struct {
	OK         bool   `json:"ok"`
	Engine     string `json:"engine,omitempty"`
	EngineName string `json:"engineName,omitempty"`
	Label      string `json:"label,omitempty"`
	MaskedDSN  string `json:"maskedDsn,omitempty"`
	TablesRead int    `json:"tablesRead,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Error      string `json:"error,omitempty"`
}

// CompareResponse carries the complete schema comparison payload for the Web UI.
type CompareResponse struct {
	OK              bool       `json:"ok"`
	ReportID        string     `json:"reportId,omitempty"`
	MarkdownSummary string     `json:"markdownSummary,omitempty"`
	Report          *ReportDTO `json:"report,omitempty"`
	Error           string     `json:"error,omitempty"`
}

// ReportDTO is the sanitized JSON representation of diff.Report.
type ReportDTO struct {
	GeneratedAt     string          `json:"generatedAt"`
	DurationMs      int64           `json:"durationMs"`
	DurationDisplay string          `json:"durationDisplay"`
	SourceA         SnapshotMetaDTO `json:"sourceA"`
	SourceB         SnapshotMetaDTO `json:"sourceB"`
	IgnoredPatterns []string        `json:"ignoredPatterns,omitempty"`
	StrictCase      bool            `json:"strictCase"`
	ParityPercent   int             `json:"parityPercent"`
	Summary         SummaryDTO      `json:"summary"`
	Tables          []TableDiffDTO  `json:"tables"`
}

// SnapshotMetaDTO holds redacted database endpoint details.
type SnapshotMetaDTO struct {
	Engine     string `json:"engine"`
	EngineName string `json:"engineName"`
	Label      string `json:"label"`
	MaskedDSN  string `json:"maskedDsn"`
}

// SummaryDTO holds aggregate diff counters.
type SummaryDTO struct {
	TablesReadA         int  `json:"tablesReadA"`
	TablesReadB         int  `json:"tablesReadB"`
	TablesIgnoredA      int  `json:"tablesIgnoredA"`
	TablesIgnoredB      int  `json:"tablesIgnoredB"`
	TablesOnlyA         int  `json:"tablesOnlyA"`
	TablesOnlyB         int  `json:"tablesOnlyB"`
	TablesModified      int  `json:"tablesModified"`
	TablesIdentical     int  `json:"tablesIdentical"`
	ColumnsOnlyA        int  `json:"columnsOnlyA"`
	ColumnsOnlyB        int  `json:"columnsOnlyB"`
	ColumnsModified     int  `json:"columnsModified"`
	ColumnsIdentical    int  `json:"columnsIdentical"`
	TypeMismatches      int  `json:"typeMismatches"`
	LengthMismatches    int  `json:"lengthMismatches"`
	PrecisionMismatches int  `json:"precisionMismatches"`
	NullableMismatches  int  `json:"nullableMismatches"`
	DefaultMismatches   int  `json:"defaultMismatches"`
	UnmappedCount       int  `json:"unmappedCount"`
	CaseDiffCount       int  `json:"caseDiffCount"`
	CrossEngine         bool `json:"crossEngine"`
	HasDrift            bool `json:"hasDrift"`
}

// TableDiffDTO represents one table's comparison status and columns.
type TableDiffDTO struct {
	AnchorID      string          `json:"anchorId"`
	Name          string          `json:"name"`
	NameA         string          `json:"nameA,omitempty"`
	NameB         string          `json:"nameB,omitempty"`
	SchemaA       string          `json:"schemaA,omitempty"`
	SchemaB       string          `json:"schemaB,omitempty"`
	Status        string          `json:"status"`
	StatusLabel   string          `json:"statusLabel"`
	CaseDiff      bool            `json:"caseDiff"`
	UnmappedCount int             `json:"unmappedCount"`
	OnlyACols     int             `json:"onlyACols"`
	OnlyBCols     int             `json:"onlyBCols"`
	ModifiedCols  int             `json:"modifiedCols"`
	IdenticalCols int             `json:"identicalCols"`
	TotalCols     int             `json:"totalCols"`
	Columns       []ColumnDiffDTO `json:"columns"`
}

// ColumnDiffDTO represents one column's comparison status across Source A and Target B.
type ColumnDiffDTO struct {
	Name                      string         `json:"name"`
	NameA                     string         `json:"nameA,omitempty"`
	NameB                     string         `json:"nameB,omitempty"`
	Status                    string         `json:"status"`
	StatusLabel               string         `json:"statusLabel"`
	ColA                      *ColumnSideDTO `json:"colA,omitempty"`
	ColB                      *ColumnSideDTO `json:"colB,omitempty"`
	Diffs                     []AttrDiffDTO  `json:"diffs,omitempty"`
	Unmapped                  bool           `json:"unmapped"`
	CaseDiff                  bool           `json:"caseDiff"`
	CrossEngineDefaultSkipped bool           `json:"crossEngineDefaultSkipped"`
}

// ColumnSideDTO describes normalized metadata on one side of a column diff.
type ColumnSideDTO struct {
	RawType       string `json:"rawType"`
	CanonicalType string `json:"canonicalType"`
	Nullable      string `json:"nullable"`
	DefaultExpr   string `json:"defaultExpr,omitempty"`
}

// AttrDiffDTO represents a single attribute difference on a matched column.
type AttrDiffDTO struct {
	Field  string `json:"field"`
	ValueA string `json:"valueA"`
	ValueB string `json:"valueB"`
}

// BuildCompareResponse transforms a diff.Report into a JSON-safe Web UI payload.
func BuildCompareResponse(reportID string, rep diff.Report) CompareResponse {
	total := len(rep.Tables)
	parity := 100
	if total > 0 {
		parity = int((float64(rep.Summary.TablesIdentical) * 100 / float64(total)) + 0.5)
	}

	tables := make([]TableDiffDTO, 0, len(rep.Tables))
	for i, td := range rep.Tables {
		cols := make([]ColumnDiffDTO, 0, len(td.Columns))
		for _, cd := range td.Columns {
			cDTO := ColumnDiffDTO{
				Name:                      cd.DisplayName(),
				NameA:                     cd.NameA,
				NameB:                     cd.NameB,
				Status:                    string(cd.Status),
				StatusLabel:               dtoStatusLabel(cd.Status),
				Unmapped:                  cd.Unmapped,
				CaseDiff:                  cd.CaseDiff,
				CrossEngineDefaultSkipped: cd.CrossEngineDefaultSkipped,
			}
			if cd.ColA != nil {
				cDTO.ColA = buildColumnSideDTO(cd.ColA)
			}
			if cd.ColB != nil {
				cDTO.ColB = buildColumnSideDTO(cd.ColB)
			}
			if len(cd.Diffs) > 0 {
				cDTO.Diffs = make([]AttrDiffDTO, 0, len(cd.Diffs))
				for _, d := range cd.Diffs {
					cDTO.Diffs = append(cDTO.Diffs, AttrDiffDTO{
						Field:  d.Field,
						ValueA: d.ValueA,
						ValueB: d.ValueB,
					})
				}
			}
			cols = append(cols, cDTO)
		}

		tables = append(tables, TableDiffDTO{
			AnchorID:      fmt.Sprintf("tbl-%d", i),
			Name:          td.DisplayName(),
			NameA:         td.NameA,
			NameB:         td.NameB,
			SchemaA:       td.SchemaA,
			SchemaB:       td.SchemaB,
			Status:        string(td.Status),
			StatusLabel:   dtoStatusLabel(td.Status),
			CaseDiff:      td.CaseDiff,
			UnmappedCount: td.UnmappedCount,
			OnlyACols:     td.OnlyACols,
			OnlyBCols:     td.OnlyBCols,
			ModifiedCols:  td.ModifiedCols,
			IdenticalCols: td.IdenticalCols,
			TotalCols:     len(td.Columns),
			Columns:       cols,
		})
	}

	return CompareResponse{
		OK:              true,
		ReportID:        reportID,
		MarkdownSummary: render.MarkdownSummary(rep),
		Report: &ReportDTO{
			GeneratedAt:     rep.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
			DurationMs:      rep.Duration.Milliseconds(),
			DurationDisplay: rep.Duration.Round(time.Millisecond).String(),
			SourceA: SnapshotMetaDTO{
				Engine:     string(rep.SourceA.Engine),
				EngineName: rep.SourceA.Engine.DisplayName(),
				Label:      rep.SourceA.Label,
				MaskedDSN:  rep.SourceA.MaskedDSN,
			},
			SourceB: SnapshotMetaDTO{
				Engine:     string(rep.SourceB.Engine),
				EngineName: rep.SourceB.Engine.DisplayName(),
				Label:      rep.SourceB.Label,
				MaskedDSN:  rep.SourceB.MaskedDSN,
			},
			IgnoredPatterns: rep.IgnoredPatterns,
			StrictCase:      rep.StrictCase,
			ParityPercent:   parity,
			Summary: SummaryDTO{
				TablesReadA:         rep.Summary.TablesReadA,
				TablesReadB:         rep.Summary.TablesReadB,
				TablesIgnoredA:      rep.Summary.TablesIgnoredA,
				TablesIgnoredB:      rep.Summary.TablesIgnoredB,
				TablesOnlyA:         rep.Summary.TablesOnlyA,
				TablesOnlyB:         rep.Summary.TablesOnlyB,
				TablesModified:      rep.Summary.TablesModified,
				TablesIdentical:     rep.Summary.TablesIdentical,
				ColumnsOnlyA:        rep.Summary.ColumnsOnlyA,
				ColumnsOnlyB:        rep.Summary.ColumnsOnlyB,
				ColumnsModified:     rep.Summary.ColumnsModified,
				ColumnsIdentical:    rep.Summary.ColumnsIdentical,
				TypeMismatches:      rep.Summary.TypeMismatches,
				LengthMismatches:    rep.Summary.LengthMismatches,
				PrecisionMismatches: rep.Summary.PrecisionMismatches,
				NullableMismatches:  rep.Summary.NullableMismatches,
				DefaultMismatches:   rep.Summary.DefaultMismatches,
				UnmappedCount:       rep.Summary.UnmappedCount,
				CaseDiffCount:       rep.Summary.CaseDiffCount,
				CrossEngine:         rep.Summary.CrossEngine,
				HasDrift:            rep.Summary.HasDrift,
			},
			Tables: tables,
		},
	}
}

func buildColumnSideDTO(c *model.Column) *ColumnSideDTO {
	nullable := "NOT NULL"
	if c.Nullable {
		nullable = "NULL"
	}
	def := ""
	if c.HasDefault {
		if c.DefaultExpr != "" {
			def = c.DefaultExpr
		} else {
			def = "<default>"
		}
	}
	return &ColumnSideDTO{
		RawType:       c.RawType,
		CanonicalType: c.FormattedCanonical(),
		Nullable:      nullable,
		DefaultExpr:   def,
	}
}

func dtoStatusLabel(s diff.DiffStatus) string {
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
