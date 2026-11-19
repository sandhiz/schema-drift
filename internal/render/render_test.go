package render

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"schema-drift/internal/diff"
	"schema-drift/internal/model"
	"schema-drift/internal/normalizer"
)

func intPtr(v int) *int { return &v }

func buildSampleReport() diff.Report {
	snapA := model.SchemaSnapshot{
		Engine:    model.EngineMSSQL,
		Label:     "sqlserver://legacy.internal:1433/AppDB",
		MaskedDSN: "sqlserver://sa:***@legacy.internal:1433?database=AppDB",
		Tables: []model.Table{
			{
				Schema:     "dbo",
				LogicalKey: "Users",
				Name:       "Users",
				Columns: []model.Column{
					normalizer.NormalizeColumn(model.EngineMSSQL, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "bigint"}),
					normalizer.NormalizeColumn(model.EngineMSSQL, normalizer.RawColumn{Name: "email", Ordinal: 2, DataType: "nvarchar", MaxBytes: intPtr(160)}),
					normalizer.NormalizeColumn(model.EngineMSSQL, normalizer.RawColumn{Name: "org_node", Ordinal: 3, DataType: "hierarchyid"}),
				},
			},
			{
				Schema:     "dbo",
				LogicalKey: "legacy_audit",
				Name:       "legacy_audit",
				Columns: []model.Column{
					normalizer.NormalizeColumn(model.EngineMSSQL, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "int"}),
				},
			},
		},
	}

	snapB := model.SchemaSnapshot{
		Engine:    model.EnginePostgres,
		Label:     "postgres://target.internal:5432/appdb",
		MaskedDSN: "postgres://postgres:***@target.internal:5432/appdb",
		Tables: []model.Table{
			{
				Schema:     "public",
				LogicalKey: "users",
				Name:       "users",
				Columns: []model.Column{
					normalizer.NormalizeColumn(model.EnginePostgres, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "bigint"}),
					normalizer.NormalizeColumn(model.EnginePostgres, normalizer.RawColumn{Name: "email", Ordinal: 2, DataType: "character varying", CharMaxLength: intPtr(120), Nullable: true}),
					normalizer.NormalizeColumn(model.EnginePostgres, normalizer.RawColumn{Name: "mfa_enabled", Ordinal: 3, DataType: "boolean"}),
				},
			},
			{
				Schema:     "public",
				LogicalKey: "webhooks",
				Name:       "webhooks",
				Columns: []model.Column{
					normalizer.NormalizeColumn(model.EnginePostgres, normalizer.RawColumn{Name: "payload", Ordinal: 1, DataType: "jsonb"}),
				},
			},
		},
	}

	rep, _ := diff.Compare(snapA, snapB, diff.Options{})
	rep.Duration = 45 * time.Millisecond
	return rep
}

func TestTerminalRender(t *testing.T) {
	rep := buildSampleReport()

	var colored bytes.Buffer
	if err := Terminal(&colored, rep, TerminalOptions{Color: true}); err != nil {
		t.Fatalf("Terminal colored error: %v", err)
	}
	if !strings.Contains(colored.String(), "\033[") {
		t.Error("expected ANSI escape sequences when Color: true")
	}

	var plain bytes.Buffer
	if err := Terminal(&plain, rep, TerminalOptions{Color: false}); err != nil {
		t.Fatalf("Terminal plain error: %v", err)
	}
	out := plain.String()
	if strings.Contains(out, "\033[") {
		t.Error("expected zero ANSI codes when Color: false")
	}
	for _, want := range []string{
		"Only in A", "Only in B", "Modified",
		"legacy_audit", "webhooks", "Users",
		"mfa_enabled", "[unmapped]", "[DRIFT]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal output missing %q", want)
		}
	}
}

func TestHTMLRenderSelfContainedAndSafe(t *testing.T) {
	rep := buildSampleReport()

	var buf bytes.Buffer
	if err := HTML(&buf, rep); err != nil {
		t.Fatalf("HTML() error: %v", err)
	}
	html := buf.String()

	// Zero external CDNs, scripts, or stylesheets (PRD US3)
	reExternalURL := regexp.MustCompile(`(?i)https?://|//cdn\.`)
	if loc := reExternalURL.FindString(html); loc != "" {
		t.Fatalf("HTML report must not reference external URLs or CDNs; found %q", loc)
	}
	if strings.Contains(strings.ToLower(html), "<script src=") {
		t.Fatal("HTML report must not include external <script src=...>")
	}
	if strings.Contains(strings.ToLower(html), `<link rel="stylesheet"`) {
		t.Fatal("HTML report must not include external <link rel=\"stylesheet\">")
	}

	// Required PRD US3 UI elements
	for _, want := range []string{
		"prefers-color-scheme: dark",
		`id="table-search"`,
		"Only in Source A",
		"Only in Source B",
		"Modified Tables",
		"unmapped",
		"legacy_audit",
		"webhooks",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML report missing required element %q", want)
		}
	}
}

func TestHTMLXSSAndPasswordProtection(t *testing.T) {
	secret := "SuperSecretPass!99"
	xssPayload := `<script>alert('xss')</script>`

	snapA := model.SchemaSnapshot{
		Engine:    model.EnginePostgres,
		Label:     "postgres://db:5432/app",
		MaskedDSN: "postgres://user:***@db:5432/app",
		Tables: []model.Table{
			{
				Schema:     "public",
				LogicalKey: xssPayload,
				Name:       xssPayload,
				Columns: []model.Column{
					{
						Name:          xssPayload,
						Ordinal:       1,
						RawType:       "varchar(50)",
						CanonicalType: "string",
						HasDefault:    true,
						DefaultExpr:   xssPayload,
					},
				},
			},
		},
	}
	snapB := model.SchemaSnapshot{
		Engine:    model.EnginePostgres,
		Label:     "postgres://db2:5432/app",
		MaskedDSN: "postgres://user:***@db2:5432/app",
	}

	rep, _ := diff.Compare(snapA, snapB, diff.Options{})
	var buf bytes.Buffer
	if err := HTML(&buf, rep); err != nil {
		t.Fatalf("HTML() error: %v", err)
	}
	html := buf.String()

	if strings.Contains(html, secret) {
		t.Fatal("HTML output must never contain secret")
	}
	if strings.Contains(html, xssPayload) {
		t.Fatal("HTML output must escape raw <script> tags in identifiers/defaults")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatal("expected escaped HTML entities for XSS test payload")
	}
}

func TestHTMLStress500TablesAndEmpty(t *testing.T) {
	// Empty schema render
	emptyRep, _ := diff.Compare(model.SchemaSnapshot{Engine: model.EnginePostgres}, model.SchemaSnapshot{Engine: model.EngineMySQL}, diff.Options{})
	var emptyBuf bytes.Buffer
	if err := HTML(&emptyBuf, emptyRep); err != nil {
		t.Fatalf("HTML empty render error: %v", err)
	}
	if !strings.Contains(emptyBuf.String(), "No tables available to compare") {
		t.Error("expected empty state message for zero tables")
	}

	// 500-table stress render
	tables := make([]model.Table, 0, 500)
	for i := 0; i < 500; i++ {
		tables = append(tables, model.Table{
			Schema:     "public",
			LogicalKey: fmt.Sprintf("tbl_%03d", i),
			Name:       fmt.Sprintf("tbl_%03d", i),
			Columns: []model.Column{
				normalizer.NormalizeColumn(model.EnginePostgres, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "bigint"}),
			},
		})
	}
	bigRep, _ := diff.Compare(
		model.SchemaSnapshot{Engine: model.EnginePostgres, Tables: tables},
		model.SchemaSnapshot{Engine: model.EnginePostgres, Tables: tables[1:]},
		diff.Options{},
	)
	var bigBuf bytes.Buffer
	if err := HTML(&bigBuf, bigRep); err != nil {
		t.Fatalf("HTML 500-table render error: %v", err)
	}
	if bigBuf.Len() == 0 {
		t.Fatal("expected non-empty HTML output for 500 tables")
	}
}
