package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"schema-drift/internal/diff"
	"schema-drift/internal/model"
)

// TerminalOptions configures CLI diff rendering.
type TerminalOptions struct {
	Color bool
}

// Terminal writes a structured, color-coded schema diff report to w.
func Terminal(w io.Writer, rep diff.Report, opts TerminalOptions) error {
	p := palette{enabled: opts.Color}
	var b strings.Builder

	rule := strings.Repeat("-", 72)

	b.WriteString("\n")
	b.WriteString(p.bold("schema-drift"))
	b.WriteString(" ")
	b.WriteString(p.dim("Schema Comparison Report"))
	b.WriteString("\n")
	b.WriteString(p.dim(rule))
	b.WriteString("\n")

	fmt.Fprintf(&b, "  %-10s %-16s %s %s\n",
		p.dim("Source A"),
		p.cyan("["+rep.SourceA.Engine.DisplayName()+"]"),
		rep.SourceA.MaskedDSN,
		p.dim(fmt.Sprintf("(%d tables read)", rep.Summary.TablesReadA)),
	)
	fmt.Fprintf(&b, "  %-10s %-16s %s %s\n",
		p.dim("Source B"),
		p.cyan("["+rep.SourceB.Engine.DisplayName()+"]"),
		rep.SourceB.MaskedDSN,
		p.dim(fmt.Sprintf("(%d tables read)", rep.Summary.TablesReadB)),
	)
	if rep.Summary.TablesIgnoredA > 0 || rep.Summary.TablesIgnoredB > 0 || len(rep.IgnoredPatterns) > 0 {
		fmt.Fprintf(&b, "  %-10s %d in A, %d in B %s\n",
			p.dim("Ignored"),
			rep.Summary.TablesIgnoredA,
			rep.Summary.TablesIgnoredB,
			p.dim("("+strings.Join(rep.IgnoredPatterns, ", ")+")"),
		)
	}
	if rep.Duration > 0 {
		fmt.Fprintf(&b, "  %-10s %s\n", p.dim("Duration"), rep.Duration.Round(time.Millisecond))
	}
	b.WriteString("\n")

	// Summary ledger
	b.WriteString(p.bold("Summary"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "  %s %-14s %3d tables",
		p.green("+"),
		p.green("Only in A"),
		rep.Summary.TablesOnlyA,
	)
	if rep.Summary.ColumnsOnlyA > 0 {
		fmt.Fprintf(&b, "   %s", p.dim(fmt.Sprintf("(+%d columns in modified tables)", rep.Summary.ColumnsOnlyA)))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "  %s %-14s %3d tables",
		p.red("-"),
		p.red("Only in B"),
		rep.Summary.TablesOnlyB,
	)
	if rep.Summary.ColumnsOnlyB > 0 {
		fmt.Fprintf(&b, "   %s", p.dim(fmt.Sprintf("(-%d columns in modified tables)", rep.Summary.ColumnsOnlyB)))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "  %s %-14s %3d tables",
		p.yellow("~"),
		p.yellow("Modified"),
		rep.Summary.TablesModified,
	)
	if rep.Summary.ColumnsModified > 0 {
		fmt.Fprintf(&b, "   %s", p.dim(fmt.Sprintf("(%d column diffs: type %d, length %d, prec/scale %d, null %d, default %d)",
			rep.Summary.ColumnsModified,
			rep.Summary.TypeMismatches,
			rep.Summary.LengthMismatches,
			rep.Summary.PrecisionMismatches,
			rep.Summary.NullableMismatches,
			rep.Summary.DefaultMismatches,
		)))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "  %s %-14s %3d tables\n",
		p.dim("="),
		p.dim("Identical"),
		rep.Summary.TablesIdentical,
	)

	if rep.Summary.UnmappedCount > 0 || rep.Summary.CaseDiffCount > 0 {
		var notes []string
		if rep.Summary.UnmappedCount > 0 {
			notes = append(notes, p.magenta(fmt.Sprintf("%d unmapped type(s)", rep.Summary.UnmappedCount)))
		}
		if rep.Summary.CaseDiffCount > 0 {
			notes = append(notes, p.cyan(fmt.Sprintf("%d casing difference(s)", rep.Summary.CaseDiffCount)))
		}
		fmt.Fprintf(&b, "  %s %-14s    %s\n", p.cyan("!"), "Notices", strings.Join(notes, p.dim(" · ")))
	}

	onlyA := rep.TablesByStatus(diff.StatusOnlyA)
	if len(onlyA) > 0 {
		b.WriteString("\n")
		b.WriteString(p.green(fmt.Sprintf("TABLES ONLY IN A (+%d)", len(onlyA))))
		b.WriteString("\n")
		for _, tbl := range onlyA {
			fmt.Fprintf(&b, "  %s %s %s\n",
				p.green("+"),
				p.bold(tbl.DisplayName()),
				p.dim(fmt.Sprintf("(%d columns)", len(tbl.Columns))),
			)
			writeCompactColPreview(&b, p, tbl.Columns, true)
		}
	}

	onlyB := rep.TablesByStatus(diff.StatusOnlyB)
	if len(onlyB) > 0 {
		b.WriteString("\n")
		b.WriteString(p.red(fmt.Sprintf("TABLES ONLY IN B (-%d)", len(onlyB))))
		b.WriteString("\n")
		for _, tbl := range onlyB {
			fmt.Fprintf(&b, "  %s %s %s\n",
				p.red("-"),
				p.bold(tbl.DisplayName()),
				p.dim(fmt.Sprintf("(%d columns)", len(tbl.Columns))),
			)
			writeCompactColPreview(&b, p, tbl.Columns, false)
		}
	}

	modified := rep.TablesByStatus(diff.StatusModified)
	if len(modified) > 0 {
		b.WriteString("\n")
		b.WriteString(p.yellow(fmt.Sprintf("MODIFIED TABLES (~%d)", len(modified))))
		b.WriteString("\n")
		for _, tbl := range modified {
			b.WriteString("  ")
			b.WriteString(p.yellow("~"))
			b.WriteString(" ")
			b.WriteString(p.bold(tbl.DisplayName()))
			if tbl.CaseDiff {
				b.WriteString(" ")
				b.WriteString(p.cyan(fmt.Sprintf("[case: %s <-> %s]", tbl.NameA, tbl.NameB)))
			}
			b.WriteString("\n")

			for _, col := range tbl.Columns {
				switch col.Status {
				case diff.StatusOnlyA:
					fmt.Fprintf(&b, "      %s %-24s %s\n",
						p.green("+"),
						p.green(col.NameA),
						formatSideColumnSummary(col.ColA, p),
					)
				case diff.StatusOnlyB:
					fmt.Fprintf(&b, "      %s %-24s %s\n",
						p.red("-"),
						p.red(col.NameB),
						formatSideColumnSummary(col.ColB, p),
					)
				case diff.StatusModified:
					fmt.Fprintf(&b, "      %s %-24s %s\n",
						p.yellow("~"),
						p.yellow(col.DisplayName()),
						formatAttrDiffLine(col, p),
					)
				default:
					if col.CaseDiff || col.Unmapped {
						var badges []string
						if col.CaseDiff {
							badges = append(badges, p.cyan(fmt.Sprintf("[case: %s <-> %s]", col.NameA, col.NameB)))
						}
						if col.Unmapped {
							badges = append(badges, p.magenta("[unmapped: "+col.ColA.RawType+"]"))
						}
						fmt.Fprintf(&b, "      %s %-24s %s\n",
							p.dim("="),
							p.dim(col.DisplayName()),
							strings.Join(badges, " "),
						)
					}
				}
			}
		}
	}

	// Highlight table-level notices on identical tables if any exist
	var noticedIdentical []diff.TableDiff
	for _, tbl := range rep.TablesByStatus(diff.StatusIdentical) {
		if tbl.CaseDiff || tbl.UnmappedCount > 0 {
			noticedIdentical = append(noticedIdentical, tbl)
		}
	}
	if len(noticedIdentical) > 0 {
		b.WriteString("\n")
		b.WriteString(p.cyan(fmt.Sprintf("NOTICES IN STRUCTURALLY IDENTICAL TABLES (%d)", len(noticedIdentical))))
		b.WriteString("\n")
		for _, tbl := range noticedIdentical {
			var tags []string
			if tbl.CaseDiff {
				tags = append(tags, p.cyan(fmt.Sprintf("case: %s <-> %s", tbl.NameA, tbl.NameB)))
			}
			if tbl.UnmappedCount > 0 {
				tags = append(tags, p.magenta(fmt.Sprintf("%d unmapped column(s)", tbl.UnmappedCount)))
			}
			fmt.Fprintf(&b, "  %s %s  %s\n", p.cyan("!"), tbl.DisplayName(), strings.Join(tags, p.dim(" · ")))
		}
	}

	b.WriteString("\n")
	b.WriteString(p.dim(rule))
	b.WriteString("\n")
	if rep.Summary.HasDrift {
		b.WriteString(p.red("[DRIFT]"))
		b.WriteString(" Schema differences detected between Source A and Source B.\n\n")
	} else {
		b.WriteString(p.green("[OK]"))
		b.WriteString(" Schemas are structurally identical.\n\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func writeCompactColPreview(b *strings.Builder, p palette, cols []diff.ColumnDiff, isA bool) {
	if len(cols) == 0 {
		return
	}
	limit := len(cols)
	if limit > 8 {
		limit = 8
	}
	parts := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		c := cols[i]
		var mc *model.Column
		name := c.NameA
		if isA {
			mc = c.ColA
		} else {
			name = c.NameB
			mc = c.ColB
		}
		if mc != nil {
			tag := name + " " + mc.RawType
			if mc.Unmapped {
				tag += " [unmapped]"
			}
			parts = append(parts, tag)
		}
	}
	line := strings.Join(parts, ", ")
	if len(cols) > limit {
		line += fmt.Sprintf(", +%d more", len(cols)-limit)
	}
	fmt.Fprintf(b, "      %s\n", p.dim(line))
}

func formatSideColumnSummary(c *model.Column, p palette) string {
	if c == nil {
		return ""
	}
	nullStr := "NOT NULL"
	if c.Nullable {
		nullStr = "NULL"
	}
	out := fmt.Sprintf("%s  %s", c.RawType, p.dim(nullStr))
	if c.HasDefault && c.DefaultExpr != "" {
		out += "  " + p.dim("DEFAULT "+c.DefaultExpr)
	}
	if c.Unmapped {
		out += "  " + p.magenta("[unmapped]")
	}
	return out
}

func formatAttrDiffLine(col diff.ColumnDiff, p palette) string {
	parts := make([]string, 0, len(col.Diffs)+2)
	for _, d := range col.Diffs {
		parts = append(parts, fmt.Sprintf("[%s: %s -> %s]", d.Field, d.ValueA, d.ValueB))
	}
	if col.Unmapped {
		parts = append(parts, p.magenta("[unmapped]"))
	}
	if col.CaseDiff && !hasField(col.Diffs, "case") {
		parts = append(parts, p.cyan(fmt.Sprintf("[case: %s <-> %s]", col.NameA, col.NameB)))
	}
	if col.CrossEngineDefaultSkipped {
		parts = append(parts, p.dim("(cross-engine defaults differ; presence matches)"))
	}
	return strings.Join(parts, " ")
}

func hasField(diffs []diff.AttrDiff, field string) bool {
	for _, d := range diffs {
		if d.Field == field {
			return true
		}
	}
	return false
}
