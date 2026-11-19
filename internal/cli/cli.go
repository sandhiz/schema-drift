package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"schema-drift/internal/diff"
	"schema-drift/internal/dsn"
	"schema-drift/internal/introspect"
	"schema-drift/internal/model"
	"schema-drift/internal/render"
	"schema-drift/internal/web"
)

// Build metadata injected at compile time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

const (
	ExitIdentical = 0
	ExitDrift     = 1
	ExitError     = 2
)

// OpenFunc opens a read-only metadata introspector for a parsed DSN.
type OpenFunc func(ctx context.Context, parsed dsn.ParsedDSN, opts introspect.Options) (introspect.Introspector, io.Closer, error)

// Dependencies allows injecting fake introspectors or custom clocks in tests.
type Dependencies struct {
	Open OpenFunc
}

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(val string) error {
	*s = append(*s, val)
	return nil
}

// Run executes the schema-drift CLI and returns exit code 0, 1, or 2.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunWithDeps(args, stdout, stderr, Dependencies{
		Open: introspect.Open,
	})
}

// RunWithDeps executes the CLI with explicit dependencies (primarily for testing).
func RunWithDeps(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if deps.Open == nil {
		deps.Open = introspect.Open
	}

	if len(args) > 0 && args[0] == "serve" {
		return runServe(args[1:], stdout, stderr, deps)
	}
	if len(args) == 0 &&
		strings.TrimSpace(os.Getenv("SCHEMA_DRIFT_A")) == "" &&
		strings.TrimSpace(os.Getenv("SCHEMA_DRIFT_B")) == "" &&
		isInteractiveWriter(stdout) {
		return runServe(nil, stdout, stderr, deps)
	}

	var (
		dsnA        string
		dsnB        string
		outHTML     string
		schemaA     string
		schemaB     string
		labelA      string
		labelB      string
		strictCase  bool
		noColor     bool
		showVersion bool
		timeout     time.Duration
		ignoreFlags stringListFlag
	)

	fs := flag.NewFlagSet("schema-drift", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&dsnA, "a", "", "Source database A DSN (or SCHEMA_DRIFT_A env var)")
	fs.StringVar(&dsnB, "b", "", "Target database B DSN (or SCHEMA_DRIFT_B env var)")
	fs.StringVar(&outHTML, "out", "", "Write self-contained HTML diff report to file (e.g. report.html)")
	fs.Var(&ignoreFlags, "ignore", "Comma-separated or repeatable glob patterns of tables to exclude (e.g. \"temp_*,audit_*\")")
	fs.StringVar(&schemaA, "schema-a", "", "Restrict Source A introspection to a single schema")
	fs.StringVar(&schemaB, "schema-b", "", "Restrict Target B introspection to a single schema")
	fs.StringVar(&labelA, "label-a", "", "Custom display label for Source A")
	fs.StringVar(&labelB, "label-b", "", "Custom display label for Target B")
	fs.BoolVar(&strictCase, "strict-case", false, "Treat table/column casing differences as schema drift (exit 1)")
	fs.BoolVar(&noColor, "no-color", false, "Disable ANSI terminal colors")
	fs.DurationVar(&timeout, "timeout", 30*time.Second, "Connection and metadata introspection timeout")
	fs.BoolVar(&showVersion, "version", false, "Print version information and exit")

	fs.Usage = func() {
		printUsage(stderr, fs)
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitIdentical
		}
		return ExitError
	}

	if showVersion {
		fmt.Fprintf(stdout, "schema-drift %s (commit %s, built %s)\n", Version, Commit, Date)
		return ExitIdentical
	}

	if strings.TrimSpace(dsnA) == "" {
		dsnA = os.Getenv("SCHEMA_DRIFT_A")
	}
	if strings.TrimSpace(dsnB) == "" {
		dsnB = os.Getenv("SCHEMA_DRIFT_B")
	}

	if strings.TrimSpace(dsnA) == "" || strings.TrimSpace(dsnB) == "" {
		fmt.Fprintln(stderr, "error: both --a (or SCHEMA_DRIFT_A) and --b (or SCHEMA_DRIFT_B) DSNs are required")
		printUsage(stderr, fs)
		return ExitError
	}

	if timeout <= 0 {
		fmt.Fprintln(stderr, "error: --timeout must be positive")
		return ExitError
	}

	// Validate ignore globs early before opening database connections.
	if _, err := diff.NewIgnoreMatcher(ignoreFlags); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return ExitError
	}

	parsedA, err := dsn.Parse(dsnA)
	if err != nil {
		fmt.Fprintf(stderr, "error parsing source A DSN: %v\n", dsn.SanitizeError(err))
		return ExitError
	}
	parsedB, err := dsn.Parse(dsnB)
	if err != nil {
		fmt.Fprintf(stderr, "error parsing source B DSN: %v\n", dsn.SanitizeError(err, parsedA.Secrets...))
		return ExitError
	}

	allSecrets := make([]string, 0, len(parsedA.Secrets)+len(parsedB.Secrets))
	allSecrets = append(allSecrets, parsedA.Secrets...)
	allSecrets = append(allSecrets, parsedB.Secrets...)

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var tablesA, tablesB []model.Table
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		intr, closer, openErr := deps.Open(gctx, parsedA, introspect.Options{Schema: schemaA})
		if openErr != nil {
			return fmt.Errorf("source A: %w", openErr)
		}
		if closer != nil {
			defer closer.Close()
		}
		tbls, queryErr := intr.Tables(gctx)
		if queryErr != nil {
			return fmt.Errorf("source A introspection: %w", queryErr)
		}
		tablesA = tbls
		return nil
	})

	g.Go(func() error {
		intr, closer, openErr := deps.Open(gctx, parsedB, introspect.Options{Schema: schemaB})
		if openErr != nil {
			return fmt.Errorf("source B: %w", openErr)
		}
		if closer != nil {
			defer closer.Close()
		}
		tbls, queryErr := intr.Tables(gctx)
		if queryErr != nil {
			return fmt.Errorf("source B introspection: %w", queryErr)
		}
		tablesB = tbls
		return nil
	})

	if err := g.Wait(); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", dsn.SanitizeError(err, allSecrets...))
		return ExitError
	}

	snapA := model.SchemaSnapshot{
		Engine:    parsedA.Engine,
		Label:     pickLabel(labelA, parsedA.DisplayLabel),
		MaskedDSN: parsedA.Masked,
		Tables:    tablesA,
	}
	snapB := model.SchemaSnapshot{
		Engine:    parsedB.Engine,
		Label:     pickLabel(labelB, parsedB.DisplayLabel),
		MaskedDSN: parsedB.Masked,
		Tables:    tablesB,
	}

	rep, err := diff.Compare(snapA, snapB, diff.Options{
		IgnorePatterns: ignoreFlags,
		StrictCase:     strictCase,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error computing diff: %v\n", dsn.SanitizeError(err, allSecrets...))
		return ExitError
	}
	rep.Duration = time.Since(started)

	useColor := render.ShouldUseColor(stdout, noColor)
	if err := render.Terminal(stdout, rep, render.TerminalOptions{Color: useColor}); err != nil {
		fmt.Fprintf(stderr, "error writing terminal output: %v\n", err)
		return ExitError
	}

	if strings.TrimSpace(outHTML) != "" {
		if err := writeHTMLReport(outHTML, rep); err != nil {
			fmt.Fprintf(stderr, "error writing HTML report to %q: %v\n", outHTML, err)
			return ExitError
		}
		fmt.Fprintf(stdout, "HTML report saved to %s\n", outHTML)
	}

	if rep.Summary.HasDrift {
		return ExitDrift
	}
	return ExitIdentical
}

func writeHTMLReport(outPath string, rep diff.Report) error {
	cleanPath := filepath.Clean(outPath)
	dir := filepath.Dir(cleanPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	tmp, err := os.CreateTemp(dir, ".schema-drift-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := render.HTML(tmp, rep); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, cleanPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func pickLabel(custom, fallback string) string {
	if strings.TrimSpace(custom) != "" {
		return strings.TrimSpace(custom)
	}
	return fallback
}

func printUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  schema-drift                                   Launch local Web UI (when interactive)")
	fmt.Fprintln(w, "  schema-drift serve [options]                   Launch local interactive Web UI")
	fmt.Fprintln(w, "  schema-drift --a <dsn> --b <dsn> [options]     Compare two schemas in terminal / HTML")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Supported engines:")
	fmt.Fprintln(w, "  PostgreSQL   postgres://user:pass@host:5432/db?sslmode=disable")
	fmt.Fprintln(w, "  MySQL        mysql://user:pass@host:3306/db  or  user:pass@tcp(host:3306)/db")
	fmt.Fprintln(w, "  SQL Server   sqlserver://user:pass@host:1433?database=db")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Options:")
	fs.PrintDefaults()
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Environment variables:")
	fmt.Fprintln(w, "  SCHEMA_DRIFT_A   Fallback DSN for source A")
	fmt.Fprintln(w, "  SCHEMA_DRIFT_B   Fallback DSN for source B")
	fmt.Fprintln(w, "  NO_COLOR         Disable ANSI color output when non-empty")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Exit codes:")
	fmt.Fprintln(w, "  0  Schemas are identical")
	fmt.Fprintln(w, "  1  Schema drift detected")
	fmt.Fprintln(w, "  2  Connection, query, or argument error")
}

func runServe(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	var (
		addr    string
		timeout time.Duration
		openUI  bool
		noOpen  bool
		verbose bool
		noColor bool
	)

	fs := flag.NewFlagSet("schema-drift serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&addr, "addr", "127.0.0.1:8080", "HTTP address for the local Web UI")
	fs.DurationVar(&timeout, "timeout", 30*time.Second, "Default database connection and metadata query timeout")
	fs.BoolVar(&openUI, "open", true, "Open the Web UI automatically in your default browser")
	fs.BoolVar(&noOpen, "no-open", false, "Do not open the browser automatically")
	fs.BoolVar(&verbose, "verbose", false, "Log executed read-only catalog SQL queries to terminal")
	fs.BoolVar(&verbose, "v", false, "Alias for --verbose")
	fs.BoolVar(&noColor, "no-color", false, "Disable ANSI terminal colors in logs")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage:")
		fmt.Fprintln(stderr, "  schema-drift serve [--addr 127.0.0.1:8080] [--no-open] [--verbose] [--timeout 30s]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitIdentical
		}
		return ExitError
	}

	if noOpen {
		openUI = false
	}

	if timeout <= 0 {
		fmt.Fprintln(stderr, "error: --timeout must be positive")
		return ExitError
	}

	useColor := render.ShouldUseColor(stdout, noColor)
	srv, err := web.NewServer(web.Config{
		Open: func(ctx context.Context, parsed dsn.ParsedDSN, opts introspect.Options) (introspect.Introspector, io.Closer, error) {
			return deps.Open(ctx, parsed, opts)
		},
		DefaultTimeout: timeout,
		Version:        Version,
		LogOutput:      stdout,
		LogColor:       useColor,
		Verbose:        verbose,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error initializing Web UI: %v\n", err)
		return ExitError
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "error listening on %s: %v\n", addr, err)
		return ExitError
	}

	url := "http://" + ln.Addr().String()
	srv.Logger().OK("Server", "Web UI listening on %s (read-only metadata mode)", url)
	if verbose {
		srv.Logger().Info("Server", "Verbose SQL catalog logging enabled (--verbose)")
	}
	srv.Logger().Info("Server", "Press Ctrl+C to stop · Live API & compare logs will appear below")

	if openUI {
		openBrowser(url)
	}

	httpServer := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if serveErr := httpServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
		close(errCh)
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case serveErr := <-errCh:
		if serveErr != nil {
			srv.Logger().Error("Server", serveErr.Error())
			return ExitError
		}
		return ExitIdentical
	case <-sigCtx.Done():
		srv.Logger().Info("Server", "Shutting down gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return ExitIdentical
	}
}

func openBrowser(targetURL string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	case "darwin":
		cmd = exec.Command("open", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	_ = cmd.Start()
}

func isInteractiveWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
