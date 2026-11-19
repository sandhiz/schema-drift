package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestResolveEndpointFormAndDSN(t *testing.T) {
	// MySQL form with special password characters
	parsed, schema, err := ResolveEndpoint(EndpointInput{
		Mode:     "form",
		Engine:   "mysql",
		Host:     "127.0.0.1",
		Port:     "3307",
		User:     "ro_user",
		Password: "P@ss#Word!99",
		Database: "app_staging",
		SSLMode:  "require",
		Schema:   "app_staging",
	}, "Source A")
	if err != nil {
		t.Fatalf("ResolveEndpoint(mysql form) error: %v", err)
	}
	if parsed.Engine != model.EngineMySQL {
		t.Errorf("Engine = %q, want mysql", parsed.Engine)
	}
	if schema != "app_staging" {
		t.Errorf("schema = %q, want app_staging", schema)
	}
	if strings.Contains(parsed.Masked, "P@ss#Word!99") {
		t.Fatalf("Masked leaked password: %s", parsed.Masked)
	}
	if !strings.Contains(parsed.DriverDSN, "tls=true") {
		t.Errorf("expected tls=true in MySQL driver DSN: %s", parsed.DriverDSN)
	}

	// Postgres form default port
	pgParsed, _, err := ResolveEndpoint(EndpointInput{
		Mode:     "form",
		Engine:   "postgres",
		Host:     "pg.internal",
		User:     "postgres",
		Password: "pw",
		Database: "core",
		SSLMode:  "disable",
	}, "Target B")
	if err != nil {
		t.Fatalf("ResolveEndpoint(postgres form) error: %v", err)
	}
	if pgParsed.DisplayLabel != "postgres://pg.internal:5432/core" {
		t.Errorf("DisplayLabel = %q", pgParsed.DisplayLabel)
	}

	// SQL Server form
	msParsed, _, err := ResolveEndpoint(EndpointInput{
		Mode:     "form",
		Engine:   "mssql",
		Host:     "sql.internal",
		User:     "sa",
		Password: "Secret#123",
		Database: "LegacyERP",
		SSLMode:  "skip-verify",
	}, "Source A")
	if err != nil {
		t.Fatalf("ResolveEndpoint(mssql form) error: %v", err)
	}
	if msParsed.Engine != model.EngineMSSQL {
		t.Errorf("Engine = %q, want mssql", msParsed.Engine)
	}

	// DSN mode
	dsnParsed, _, err := ResolveEndpoint(EndpointInput{
		Mode: "dsn",
		DSN:  "mysql://root:myPw#1@localhost:3306/db1",
	}, "Source A")
	if err != nil {
		t.Fatalf("ResolveEndpoint(dsn mode) error: %v", err)
	}
	if dsnParsed.Engine != model.EngineMySQL {
		t.Errorf("Engine = %q, want mysql", dsnParsed.Engine)
	}

	// Missing fields errors
	for _, bad := range []EndpointInput{
		{Mode: "form", Engine: "", Host: "localhost", Database: "db"},
		{Mode: "form", Engine: "mysql", Host: "", Database: "db"},
		{Mode: "form", Engine: "mysql", Host: "localhost", Database: ""},
		{Mode: "dsn", DSN: ""},
	} {
		if _, _, err := ResolveEndpoint(bad, "Source A"); err == nil {
			t.Errorf("expected validation error for %+v", bad)
		}
	}
}

func TestWebHandlers(t *testing.T) {
	secretA := "SecretMysqlA#2026"
	secretB := "SecretMysqlB!2026"

	tablesA := []model.Table{
		{
			Schema: "app_a", LogicalKey: "users", Name: "users",
			Columns: []model.Column{
				normalizer.NormalizeColumn(model.EngineMySQL, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "bigint"}),
				normalizer.NormalizeColumn(model.EngineMySQL, normalizer.RawColumn{Name: "email", Ordinal: 2, DataType: "varchar", CharMaxLength: intp(80)}),
			},
		},
		{
			Schema: "app_a", LogicalKey: "legacy_logs", Name: "legacy_logs",
			Columns: []model.Column{
				normalizer.NormalizeColumn(model.EngineMySQL, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "int"}),
			},
		},
	}

	tablesB := []model.Table{
		{
			Schema: "app_b", LogicalKey: "users", Name: "users",
			Columns: []model.Column{
				normalizer.NormalizeColumn(model.EngineMySQL, normalizer.RawColumn{Name: "id", Ordinal: 1, DataType: "bigint"}),
				normalizer.NormalizeColumn(model.EngineMySQL, normalizer.RawColumn{Name: "email", Ordinal: 2, DataType: "varchar", CharMaxLength: intp(120)}),
			},
		},
	}

	var logBuf bytes.Buffer
	srv, err := NewServer(Config{
		LogOutput: &logBuf,
		LogColor:  false,
		Verbose:   true,
		Open: func(_ context.Context, parsed dsn.ParsedDSN, opts introspect.Options) (introspect.Introspector, io.Closer, error) {
			if opts.Observer != nil {
				opts.Observer(parsed.Engine, "SELECT table_name FROM information_schema.tables")
			}
			if strings.Contains(parsed.DisplayLabel, "bad-host") {
				return nil, nil, errors.New("dial tcp bad-host:3306 failed with password=" + secretA)
			}
			if strings.Contains(parsed.DisplayLabel, "app_a") {
				return fakeIntrospector{tables: tablesA}, nil, nil
			}
			return fakeIntrospector{tables: tablesB}, nil, nil
		},
	})
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}
	h := srv.Handler()

	// 1. GET / serves self-contained UI with zero external URLs
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rec.Code)
	}
	body := strings.ReplaceAll(rec.Body.String(), "http://www.w3.org/2000/svg", "")
	lowerBody := strings.ToLower(body)
	if strings.Contains(lowerBody, "https://") || strings.Contains(lowerBody, "http://") || strings.Contains(lowerBody, "<script src=") {
		t.Fatal("Web UI HTML must be self-contained without external http/https references")
	}

	// 2. POST /api/test-connection (success + error secret scrubbing)
	testBody, _ := json.Marshal(testConnectionRequest{
		Side: "Source A",
		Endpoint: EndpointInput{
			Mode: "form", Engine: "mysql", Host: "127.0.0.1", Port: "3306", User: "root", Password: secretA, Database: "app_a",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/test-connection", bytes.NewReader(testBody))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var tcResp TestConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &tcResp); err != nil {
		t.Fatalf("decode test-connection: %v", err)
	}
	if !tcResp.OK || tcResp.TablesRead != 2 {
		t.Fatalf("unexpected test-connection resp: %+v", tcResp)
	}
	if strings.Contains(rec.Body.String(), secretA) {
		t.Fatal("test-connection response leaked secret")
	}

	badBody, _ := json.Marshal(testConnectionRequest{
		Side: "Source A",
		Endpoint: EndpointInput{
			Mode: "form", Engine: "mysql", Host: "bad-host", Port: "3306", User: "root", Password: secretA, Database: "app_a",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/test-connection", bytes.NewReader(badBody))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), secretA) {
		t.Fatalf("failed test-connection leaked password: %s", rec.Body.String())
	}

	// 3. POST /api/compare
	cmpBody, _ := json.Marshal(compareRequest{
		SourceA: EndpointInput{Mode: "form", Engine: "mysql", Host: "127.0.0.1", Port: "3306", User: "root", Password: secretA, Database: "app_a"},
		SourceB: EndpointInput{Mode: "form", Engine: "mysql", Host: "127.0.0.1", Port: "3306", User: "root", Password: secretB, Database: "app_b"},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/compare", bytes.NewReader(cmpBody))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/compare status = %d", rec.Code)
	}
	rawResp := rec.Body.String()
	if strings.Contains(rawResp, secretA) || strings.Contains(rawResp, secretB) {
		t.Fatal("compare response leaked database password")
	}
	var cmpResp CompareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &cmpResp); err != nil {
		t.Fatalf("decode compare resp: %v", err)
	}
	if !cmpResp.OK || cmpResp.ReportID == "" || cmpResp.Report == nil || !cmpResp.Report.Summary.HasDrift {
		t.Fatalf("unexpected compare response: %+v", cmpResp)
	}
	if cmpResp.Report.Summary.TablesOnlyA != 1 || cmpResp.Report.Summary.TablesModified != 1 {
		t.Fatalf("unexpected compare summary: %+v", cmpResp.Report.Summary)
	}

	// 4. GET /api/reports/{id}/download
	req = httptest.NewRequest(http.MethodGet, "/api/reports/"+cmpResp.ReportID+"/download", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d, want 200", rec.Code)
	}
	dlBody := rec.Body.String()
	if !strings.Contains(dlBody, "legacy_logs") || strings.Contains(dlBody, secretA) || strings.Contains(dlBody, secretB) {
		t.Fatal("downloaded report missing expected table or leaked secret")
	}

	// 5. POST /api/demo
	req = httptest.NewRequest(http.MethodPost, "/api/demo", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/demo status = %d", rec.Code)
	}
	var demoResp CompareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &demoResp); err != nil || !demoResp.OK || demoResp.ReportID == "" {
		t.Fatalf("unexpected /api/demo response: %v %+v", err, demoResp)
	}

	// 6. Unknown report ID returns 404
	req = httptest.NewRequest(http.MethodGet, "/api/reports/nonexistent/download", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown report download status = %d, want 404", rec.Code)
	}

	// 7. Live logs endpoint, SQL verbose logging, and zero password leakage
	logsOut := logBuf.String()
	if strings.Contains(logsOut, secretA) || strings.Contains(logsOut, secretB) {
		t.Fatal("terminal log buffer leaked database password")
	}
	for _, wantLog := range []string{"[schema-drift]", "[Test", "[Diff", "[Introspect]", "[SQL:mysql", "[Export", "[Demo"} {
		if !strings.Contains(logsOut, wantLog) {
			t.Errorf("terminal log missing %q; got:\n%s", wantLog, logsOut)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/logs status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secretA) || strings.Contains(rec.Body.String(), secretB) {
		t.Fatal("GET /api/logs leaked database password")
	}
	var logsResp struct {
		OK   bool       `json:"ok"`
		Logs []LogEntry `json:"logs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &logsResp); err != nil || !logsResp.OK || len(logsResp.Logs) == 0 {
		t.Fatalf("unexpected /api/logs response: %v %s", err, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/logs", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/logs status = %d", rec.Code)
	}
	if len(srv.Logger().Entries()) != 0 {
		t.Fatalf("expected logs cleared after DELETE /api/logs")
	}
}
