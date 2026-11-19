package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
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
	if !strings.Contains(errBuf.String(), "-port") || (!strings.Contains(errBuf.String(), "-p\t") && !strings.Contains(errBuf.String(), "-p ")) {
		t.Errorf("serve --help missing -port or -p flags: %s", errBuf.String())
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

	for _, badArgs := range [][]string{
		{"--port", "70000"},
		{"-p", "-5"},
	} {
		out.Reset()
		errBuf.Reset()
		code = Run(badArgs, &out, &errBuf)
		if code != ExitError {
			t.Fatalf("Run(%v) = %d, want 2 (stderr: %s)", badArgs, code, errBuf.String())
		}
	}
}

func TestResolveServeAddr(t *testing.T) {
	makeEnv := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cases := []struct {
		name     string
		addrFlag string
		addrSet  bool
		portFlag int
		portSet  bool
		env      map[string]string
		wantAddr string
		wantExp  bool
		wantErr  bool
	}{
		{
			name:     "default",
			addrFlag: "127.0.0.1:8080",
			portFlag: -1,
			wantAddr: "127.0.0.1:8080",
			wantExp:  false,
		},
		{
			name:     "PORT env",
			addrFlag: "127.0.0.1:8080",
			portFlag: -1,
			env:      map[string]string{"PORT": "3000"},
			wantAddr: "127.0.0.1:3000",
			wantExp:  true,
		},
		{
			name:     "SCHEMA_DRIFT_PORT overrides PORT",
			addrFlag: "127.0.0.1:8080",
			portFlag: -1,
			env:      map[string]string{"PORT": "3000", "SCHEMA_DRIFT_PORT": "4000"},
			wantAddr: "127.0.0.1:4000",
			wantExp:  true,
		},
		{
			name:     "SCHEMA_DRIFT_ADDR overrides SCHEMA_DRIFT_PORT",
			addrFlag: "127.0.0.1:8080",
			portFlag: -1,
			env:      map[string]string{"SCHEMA_DRIFT_PORT": "4000", "SCHEMA_DRIFT_ADDR": "0.0.0.0:9000"},
			wantAddr: "0.0.0.0:9000",
			wantExp:  true,
		},
		{
			name:     "--port overrides env",
			addrFlag: "127.0.0.1:8080",
			portFlag: 3000,
			portSet:  true,
			env:      map[string]string{"PORT": "4000", "SCHEMA_DRIFT_ADDR": "0.0.0.0:9000"},
			wantAddr: "0.0.0.0:3000",
			wantExp:  true,
		},
		{
			name:     "--addr and --port combine host and port",
			addrFlag: ":8080",
			addrSet:  true,
			portFlag: 3000,
			portSet:  true,
			wantAddr: ":3000",
			wantExp:  true,
		},
		{
			name:     "--port 0 allowed",
			addrFlag: "127.0.0.1:8080",
			portFlag: 0,
			portSet:  true,
			wantAddr: "127.0.0.1:0",
			wantExp:  true,
		},
		{
			name:     "invalid --port negative",
			addrFlag: "127.0.0.1:8080",
			portFlag: -1,
			portSet:  true,
			wantErr:  true,
		},
		{
			name:     "invalid --port too high",
			addrFlag: "127.0.0.1:8080",
			portFlag: 70000,
			portSet:  true,
			wantErr:  true,
		},
		{
			name:     "invalid PORT env",
			addrFlag: "127.0.0.1:8080",
			portFlag: -1,
			env:      map[string]string{"PORT": "abc"},
			wantErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveServeAddr(tc.addrFlag, tc.addrSet, tc.portFlag, tc.portSet, makeEnv(tc.env))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Addr != tc.wantAddr || got.Explicit != tc.wantExp {
				t.Fatalf("got %+v, want Addr=%q Explicit=%v", got, tc.wantAddr, tc.wantExp)
			}
		})
	}
}

func TestListenServeFallback(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen setup error: %v", err)
	}
	defer busy.Close()

	busyAddr := busy.Addr().String()
	if ln, fellBack, err := listenServe(busyAddr, false); err == nil {
		ln.Close()
		t.Fatalf("listenServe(%s, false) unexpectedly succeeded (fellBack=%v)", busyAddr, fellBack)
	}

	fb, fellBack, err := listenServe(busyAddr, true)
	if err != nil {
		t.Fatalf("listenServe(%s, true) failed: %v", busyAddr, err)
	}
	defer fb.Close()

	if !fellBack {
		t.Fatalf("expected fellBack=true when binding busy %s", busyAddr)
	}
	if fb.Addr().String() == busyAddr {
		t.Fatalf("fallback listener bound to same busy address %s", busyAddr)
	}
}

type fakeNetAddr string

func (f fakeNetAddr) Network() string { return "tcp" }
func (f fakeNetAddr) String() string  { return string(f) }

func TestBuildBrowserURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"127.0.0.1:3000", "http://127.0.0.1:3000"},
		{"0.0.0.0:3000", "http://127.0.0.1:3000"},
		{"[::]:3000", "http://127.0.0.1:3000"},
		{":3000", "http://127.0.0.1:3000"},
	}
	for _, tc := range cases {
		if got := buildBrowserURL(fakeNetAddr(tc.in)); got != tc.want {
			t.Errorf("buildBrowserURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsServeShorthandArgs(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"-p", "3000"}, true},
		{[]string{"--port=9090"}, true},
		{[]string{"--addr", ":4000"}, true},
		{[]string{"-v"}, true},
		{[]string{"--help"}, false},
		{[]string{"--version"}, false},
		{[]string{"--a", "postgres://u:p@h/a", "--b", "postgres://u:p@h/b"}, false},
		{[]string{"--a", "postgres://u:p@h/a", "--port", "3000"}, false},
	}
	for _, tc := range cases {
		if got := isServeShorthandArgs(tc.args); got != tc.want {
			t.Errorf("isServeShorthandArgs(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
