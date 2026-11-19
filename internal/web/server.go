package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"schema-drift/internal/demo"
	"schema-drift/internal/diff"
	"schema-drift/internal/dsn"
	"schema-drift/internal/introspect"
	"schema-drift/internal/model"
	"schema-drift/internal/render"
)

//go:embed templates/app.html
var appTemplateFS embed.FS

// OpenFunc opens a read-only metadata introspector for a parsed DSN.
type OpenFunc func(ctx context.Context, parsed dsn.ParsedDSN, opts introspect.Options) (introspect.Introspector, io.Closer, error)

// Config configures the local schema-drift Web UI server.
type Config struct {
	Open           OpenFunc
	DefaultTimeout time.Duration
	Version        string
	LogOutput      io.Writer
	LogColor       bool
	Verbose        bool
}

const (
	maxBodyBytes     = 1 << 20 // 1 MiB
	maxCachedReports = 10
)

// Server serves the interactive Web UI and read-only schema comparison APIs.
type Server struct {
	cfg     Config
	appHTML []byte
	logger  *Logger

	mu      sync.RWMutex
	order   []string
	reports map[string]diff.Report
}

// NewServer constructs a Web UI server with embedded static assets.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Open == nil {
		cfg.Open = introspect.Open
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	htmlBytes, err := appTemplateFS.ReadFile("templates/app.html")
	if err != nil {
		return nil, fmt.Errorf("load embedded web UI template: %w", err)
	}
	return &Server{
		cfg:     cfg,
		appHTML: htmlBytes,
		logger:  NewLogger(cfg.LogOutput, cfg.LogColor),
		reports: make(map[string]diff.Report, maxCachedReports),
	}, nil
}

// Logger returns the server's structured live logger.
func (s *Server) Logger() *Logger {
	return s.logger
}

// Handler returns the configured HTTP router for the Web UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/test-connection", s.handleTestConnection)
	mux.HandleFunc("/api/compare", s.handleCompare)
	mux.HandleFunc("/api/demo", s.handleDemo)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/reports/", s.handleReportDownload)
	return withSecurityHeaders(s.withAccessLog(mux))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (s *Server) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/logs" || r.URL.Path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		dur := time.Since(started).Round(time.Millisecond)
		if rec.status >= 400 {
			s.logger.Warn("HTTP", "%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, dur)
		} else {
			s.logger.Info("HTTP", "%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, dur)
		}
	})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(s.appHTML)
	}
}

type testConnectionRequest struct {
	Endpoint   EndpointInput `json:"endpoint"`
	Side       string        `json:"side"`
	TimeoutSec int           `json:"timeoutSec"`
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req testConnectionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request payload: "+err.Error())
		return
	}

	side := strings.TrimSpace(req.Side)
	if side == "" {
		side = "Database"
	}

	parsed, schema, err := ResolveEndpoint(req.Endpoint, side)
	if err != nil {
		s.logger.Error("Test", err.Error(), req.Endpoint.Password)
		writeJSON(w, http.StatusBadRequest, TestConnectionResponse{OK: false, Error: err.Error()})
		return
	}

	s.logger.Info("Test", "Connecting %s [%s] %s", side, parsed.Engine.DisplayName(), parsed.DisplayLabel)
	timeout := s.pickTimeout(req.TimeoutSec)
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var observer introspect.QueryObserver
	if s.cfg.Verbose {
		observer = func(eng model.Engine, q string) {
			s.logger.SQL("SQL:"+string(eng), q)
		}
	}

	started := time.Now()
	intr, closer, err := s.cfg.Open(ctx, parsed, introspect.Options{Schema: schema, Observer: observer})
	if err != nil {
		cleanErr := dsn.SanitizeError(err, parsed.Secrets...).Error()
		s.logger.Error("Test", fmt.Sprintf("%s connection failed: %s", side, cleanErr), parsed.Secrets...)
		writeJSON(w, http.StatusOK, TestConnectionResponse{
			OK:    false,
			Error: cleanErr,
		})
		return
	}
	if closer != nil {
		defer closer.Close()
	}

	tables, err := intr.Tables(ctx)
	if err != nil {
		cleanErr := dsn.SanitizeError(err, parsed.Secrets...).Error()
		s.logger.Error("Test", fmt.Sprintf("%s catalog read failed: %s", side, cleanErr), parsed.Secrets...)
		writeJSON(w, http.StatusOK, TestConnectionResponse{
			OK:    false,
			Error: cleanErr,
		})
		return
	}

	dur := time.Since(started).Round(time.Millisecond)
	s.logger.OK("Test", "%s connected (%s) · %d table(s) visible (%s)", side, parsed.Engine.DisplayName(), len(tables), dur)

	writeJSON(w, http.StatusOK, TestConnectionResponse{
		OK:         true,
		Engine:     string(parsed.Engine),
		EngineName: parsed.Engine.DisplayName(),
		Label:      parsed.DisplayLabel,
		MaskedDSN:  parsed.Masked,
		TablesRead: len(tables),
		DurationMs: dur.Milliseconds(),
	})
}

type compareRequest struct {
	SourceA    EndpointInput `json:"sourceA"`
	SourceB    EndpointInput `json:"sourceB"`
	Ignore     string        `json:"ignore"`
	StrictCase bool          `json:"strictCase"`
	TimeoutSec int           `json:"timeoutSec"`
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req compareRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request payload: "+err.Error())
		return
	}

	parsedA, schemaA, err := ResolveEndpoint(req.SourceA, "Source A")
	if err != nil {
		s.logger.Error("Diff", err.Error(), req.SourceA.Password)
		writeJSON(w, http.StatusBadRequest, CompareResponse{OK: false, Error: err.Error()})
		return
	}
	parsedB, schemaB, err := ResolveEndpoint(req.SourceB, "Target B")
	if err != nil {
		s.logger.Error("Diff", err.Error(), req.SourceB.Password)
		writeJSON(w, http.StatusBadRequest, CompareResponse{OK: false, Error: err.Error()})
		return
	}

	var ignorePatterns []string
	if strings.TrimSpace(req.Ignore) != "" {
		ignorePatterns = []string{req.Ignore}
	}
	if _, err := diff.NewIgnoreMatcher(ignorePatterns); err != nil {
		s.logger.Error("Diff", err.Error())
		writeJSON(w, http.StatusBadRequest, CompareResponse{OK: false, Error: err.Error()})
		return
	}

	allSecrets := append([]string{}, parsedA.Secrets...)
	allSecrets = append(allSecrets, parsedB.Secrets...)

	s.logger.Info("Diff", "Comparing A=[%s] %s vs B=[%s] %s",
		parsedA.Engine.DisplayName(), parsedA.DisplayLabel,
		parsedB.Engine.DisplayName(), parsedB.DisplayLabel,
	)

	var observer introspect.QueryObserver
	if s.cfg.Verbose {
		observer = func(eng model.Engine, q string) {
			s.logger.SQL("SQL:"+string(eng), q)
		}
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), s.pickTimeout(req.TimeoutSec))
	defer cancel()

	var tablesA, tablesB []model.Table
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		t0 := time.Now()
		intr, closer, openErr := s.cfg.Open(gctx, parsedA, introspect.Options{Schema: schemaA, Observer: observer})
		if openErr != nil {
			return fmt.Errorf("Source A: %w", openErr)
		}
		if closer != nil {
			defer closer.Close()
		}
		tbls, queryErr := intr.Tables(gctx)
		if queryErr != nil {
			return fmt.Errorf("Source A metadata query: %w", queryErr)
		}
		tablesA = tbls
		s.logger.Info("Introspect", "Source A read %d table(s) in %s", len(tbls), time.Since(t0).Round(time.Millisecond))
		return nil
	})

	g.Go(func() error {
		t0 := time.Now()
		intr, closer, openErr := s.cfg.Open(gctx, parsedB, introspect.Options{Schema: schemaB, Observer: observer})
		if openErr != nil {
			return fmt.Errorf("Target B: %w", openErr)
		}
		if closer != nil {
			defer closer.Close()
		}
		tbls, queryErr := intr.Tables(gctx)
		if queryErr != nil {
			return fmt.Errorf("Target B metadata query: %w", queryErr)
		}
		tablesB = tbls
		s.logger.Info("Introspect", "Target B read %d table(s) in %s", len(tbls), time.Since(t0).Round(time.Millisecond))
		return nil
	})

	if err := g.Wait(); err != nil {
		cleanErr := dsn.SanitizeError(err, allSecrets...).Error()
		s.logger.Error("Diff", cleanErr, allSecrets...)
		writeJSON(w, http.StatusOK, CompareResponse{
			OK:    false,
			Error: cleanErr,
		})
		return
	}

	snapA := model.SchemaSnapshot{
		Engine:    parsedA.Engine,
		Label:     parsedA.DisplayLabel,
		MaskedDSN: parsedA.Masked,
		Tables:    tablesA,
	}
	snapB := model.SchemaSnapshot{
		Engine:    parsedB.Engine,
		Label:     parsedB.DisplayLabel,
		MaskedDSN: parsedB.Masked,
		Tables:    tablesB,
	}

	rep, err := diff.Compare(snapA, snapB, diff.Options{
		IgnorePatterns: ignorePatterns,
		StrictCase:     req.StrictCase,
	})
	if err != nil {
		s.logger.Error("Diff", err.Error(), allSecrets...)
		writeJSON(w, http.StatusBadRequest, CompareResponse{OK: false, Error: err.Error()})
		return
	}
	rep.Duration = time.Since(started)

	if rep.Summary.HasDrift {
		s.logger.Warn("Diff", "Drift detected: onlyA=%d, onlyB=%d, modified=%d, identical=%d (%s)",
			rep.Summary.TablesOnlyA, rep.Summary.TablesOnlyB, rep.Summary.TablesModified, rep.Summary.TablesIdentical,
			rep.Duration.Round(time.Millisecond),
		)
	} else {
		s.logger.OK("Diff", "Schemas identical across %d table(s) (%s)", rep.Summary.TablesIdentical, rep.Duration.Round(time.Millisecond))
	}

	id := s.storeReport(rep)
	writeJSON(w, http.StatusOK, BuildCompareResponse(id, rep))
}

func (s *Server) handleDemo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	snapA, snapB := demo.BuildSnapshots()
	rep, err := diff.Compare(snapA, snapB, diff.Options{
		IgnorePatterns: []string{"temp_*", "audit_*"},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, CompareResponse{OK: false, Error: err.Error()})
		return
	}
	rep.Duration = 38 * time.Millisecond
	s.logger.OK("Demo", "Loaded sample diff (SQL Server -> PostgreSQL): onlyA=%d, onlyB=%d, modified=%d, identical=%d",
		rep.Summary.TablesOnlyA, rep.Summary.TablesOnlyB, rep.Summary.TablesModified, rep.Summary.TablesIdentical)

	id := s.storeReport(rep)
	writeJSON(w, http.StatusOK, BuildCompareResponse(id, rep))
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":   true,
			"logs": s.logger.Entries(),
		})
	case http.MethodDelete:
		s.logger.Clear()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleReportDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/reports/")
	if !strings.HasSuffix(rest, "/download") {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(rest, "/download")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	rep, ok := s.getReport(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	s.logger.OK("Export", "Downloaded standalone report.html (id=%s)", id)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="schema-drift-report.html"`)
	_ = render.HTML(w, rep)
}

func (s *Server) storeReport(rep diff.Report) string {
	id := randomID()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.order) >= maxCachedReports {
		evict := s.order[0]
		s.order = s.order[1:]
		delete(s.reports, evict)
	}
	s.order = append(s.order, id)
	s.reports[id] = rep
	return id
}

func (s *Server) getReport(id string) (diff.Report, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rep, ok := s.reports[id]
	return rep, ok
}

func (s *Server) pickTimeout(sec int) time.Duration {
	if sec <= 0 {
		return s.cfg.DefaultTimeout
	}
	if sec > 300 {
		sec = 300
	}
	return time.Duration(sec) * time.Second
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

func randomID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("rep-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
