package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schema-drift/internal/dsn"
	"schema-drift/internal/introspect"
	"schema-drift/internal/model"
	"schema-drift/internal/normalizer"
)

type fakeIntrospector struct {
	tables []model.Table
	err    error
}

func (f fakeIntrospector) Tables(_ context.Context) ([]model.Table, error) {
	return f.tables, f.err
}

func intp(v int) *int { return &v }

func TestCLIHelpAndVersion(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := Run([]string{"--version"}, &out, &errBuf)
	if code != ExitIdentical {
		t.Fatalf("--version exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "schema-drift") {
		t.Errorf("--version output = %q", out.String())
	}

	out.Reset()
	errBuf.Reset()
	code = Run([]string{"--help"}, &out, &errBuf)
	if code != ExitIdentical {
		t.Fatalf("--help exit = %d, want 0", code)
	}
}

func TestCLIValidationErrorsReturnExit2(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"missing DSNs", nil},
		{"missing B", []string{"--a", "postgres://u:p@host:5432/db"}},
		{"invalid timeout", []string{"--a", "postgres://u:p@host:5432/db", "--b", "postgres://u:p@host:5432/db", "--timeout", "0s"}},
		{"invalid glob", []string{"--a", "postgres://u:p@host:5432/db", "--b", "postgres://u:p@host:5432/db", "--ignore", "[bad"}},
		{"invalid scheme", []string{"--a", "sqlite:///foo.db", "--b", "postgres://u:p@host:5432/db"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SCHEMA_DRIFT_A", "")
			t.Setenv("SCHEMA_DRIFT_B", "")
			var out, errBuf bytes.Buffer
			code := Run(tc.args, &out, &errBuf)
			if code != ExitError {
				t.Fatalf("Run(%v) = %d, want %d (stderr: %s)", tc.args, code, ExitError, errBuf.String())
			}
		})
	}
}

func TestCLIIdenticalDriftAndHTMLReport(t *testing.T) {
	secretA := "SuperSecretA!2026"
	secretB := "SuperSecretB#2026"
	dsnA := "sqlserver://sa:" + secretA + "@sql.corp:1433?database=LegacyDB"
	dsnB := "postgres://pg:" + secretB + "@pg.corp:5432/appdb"

	tablesA := []model.Table{
		{
			Schema:     "dbo",
			LogicalKey: "Users",
			Name:       "Users",
			Columns: []model.Column{
				normalizer.NormalizeColumn(model.EngineMSSQL, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "bigint"}),
				normalizer.NormalizeColumn(model.EngineMSSQL, normalizer.RawColumn{Name: "email", Ordinal: 2, DataType: "nvarchar", MaxBytes: intp(100)}),
			},
		},
		{
			Schema:     "dbo",
			LogicalKey: "temp_staging",
			Name:       "temp_staging",
			Columns:    []model.Column{normalizer.NormalizeColumn(model.EngineMSSQL, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "int"})},
		},
	}

	tablesBIdentical := []model.Table{
		{
			Schema:     "public",
			LogicalKey: "users",
			Name:       "users",
			Columns: []model.Column{
				normalizer.NormalizeColumn(model.EnginePostgres, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "bigint"}),
				normalizer.NormalizeColumn(model.EnginePostgres, normalizer.RawColumn{Name: "email", Ordinal: 2, DataType: "character varying", CharMaxLength: intp(50)}),
			},
		},
	}

	depsIdentical := Dependencies{
		Open: func(_ context.Context, parsed dsn.ParsedDSN, _ introspect.Options) (introspect.Introspector, io.Closer, error) {
			if parsed.Engine == model.EngineMSSQL {
				return fakeIntrospector{tables: tablesA}, nil, nil
			}
			return fakeIntrospector{tables: tablesBIdentical}, nil, nil
		},
	}

	// 1. Env var fallback + --ignore makes them structurally identical -> exit 0
	t.Setenv("SCHEMA_DRIFT_A", dsnA)
	t.Setenv("SCHEMA_DRIFT_B", dsnB)

	var out, errBuf bytes.Buffer
	code := RunWithDeps([]string{"--ignore", "temp_*", "--no-color"}, &out, &errBuf, depsIdentical)
	if code != ExitIdentical {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, errBuf.String())
	}
	if strings.Contains(out.String(), secretA) || strings.Contains(out.String(), secretB) {
		t.Fatal("stdout leaked raw database password")
	}

	// 2. Without --ignore, temp_staging is only in A -> exit 1 + HTML report written
	outPath := filepath.Join(t.TempDir(), "nested", "report.html")
	out.Reset()
	errBuf.Reset()
	code = RunWithDeps([]string{"--a", dsnA, "--b", dsnB, "--out", outPath, "--no-color"}, &out, &errBuf, depsIdentical)
	if code != ExitDrift {
		t.Fatalf("expected exit 1, got %d (stderr: %s)", code, errBuf.String())
	}

	htmlBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", outPath, err)
	}
	html := string(htmlBytes)
	if strings.Contains(html, secretA) || strings.Contains(html, secretB) {
		t.Fatal("HTML report leaked raw database password")
	}
	if !strings.Contains(html, "temp_staging") {
		t.Error("HTML report missing drifted table temp_staging")
	}

	// 3. --strict-case triggers exit 1 even when temp_staging is ignored
	out.Reset()
	errBuf.Reset()
	code = RunWithDeps([]string{"--a", dsnA, "--b", dsnB, "--ignore", "temp_*", "--strict-case"}, &out, &errBuf, depsIdentical)
	if code != ExitDrift {
		t.Fatalf("expected exit 1 under --strict-case, got %d", code)
	}
}

func TestCLIConnectionErrorRedactsPassword(t *testing.T) {
	secret := "MyTopSecretPass#42"
	dsnA := "postgres://admin:" + secret + "@localhost:5432/app"
	dsnB := "mysql://admin:" + secret + "@localhost:3306/app"

	deps := Dependencies{
		Open: func(_ context.Context, parsed dsn.ParsedDSN, _ introspect.Options) (introspect.Introspector, io.Closer, error) {
			return nil, nil, errors.New("connection refused for " + dsnA + " password=" + secret)
		},
	}

	var out, errBuf bytes.Buffer
	code := RunWithDeps([]string{"--a", dsnA, "--b", dsnB}, &out, &errBuf, deps)
	if code != ExitError {
		t.Fatalf("expected exit 2 on connection error, got %d", code)
	}
	if strings.Contains(errBuf.String(), secret) {
		t.Fatalf("stderr leaked raw password: %s", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "***") {
		t.Fatalf("expected masked '***' in stderr: %s", errBuf.String())
	}
}

func TestCLIServeHelpAndErrors(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := Run([]string{"serve", "--help"}, &out, &errBuf)
	if code != ExitIdentical {
		t.Fatalf("serve --help exit = %d, want 0", code)
	}
	if !strings.Contains(errBuf.String(), "schema-drift serve") {
		t.Errorf("serve --help usage missing expected header: %s", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "-verbose") || !strings.Contains(errBuf.String(), "-no-color") || !strings.Contains(errBuf.String(), "-no-open") {
		t.Errorf("serve --help missing -verbose, -no-color, or -no-open flags: %s", errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	code = Run([]string{"serve", "--timeout", "0s"}, &out, &errBuf)
	if code != ExitError {
		t.Fatalf("serve invalid timeout exit = %d, want 2", code)
	}

	out.Reset()
	errBuf.Reset()
	code = Run([]string{"serve", "--addr", "127.0.0.1:-1"}, &out, &errBuf)
	if code != ExitError {
		t.Fatalf("serve invalid addr exit = %d, want 2", code)
	}
}
