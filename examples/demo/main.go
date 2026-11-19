// Command demo generates a realistic cross-engine schema diff showcase
// (SQL Server legacy database -> PostgreSQL 16 target) without requiring live databases.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"schema-drift/internal/demo"
	"schema-drift/internal/diff"
	"schema-drift/internal/render"
)

func main() {
	outPath := flag.String("out", "docs/demo-report.html", "Output path for demo HTML report")
	noColor := flag.Bool("no-color", false, "Disable ANSI terminal colors")
	flag.Parse()

	snapA, snapB := demo.BuildSnapshots()
	rep, err := diff.Compare(snapA, snapB, diff.Options{
		IgnorePatterns: []string{"temp_*", "audit_*"},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building demo diff: %v\n", err)
		os.Exit(2)
	}
	rep.Duration = 38 * time.Millisecond

	useColor := render.ShouldUseColor(os.Stdout, *noColor)
	if err := render.Terminal(os.Stdout, rep, render.TerminalOptions{Color: useColor}); err != nil {
		fmt.Fprintf(os.Stderr, "error rendering terminal output: %v\n", err)
		os.Exit(2)
	}

	if *outPath != "" {
		dir := filepath.Dir(*outPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "error creating output directory: %v\n", err)
				os.Exit(2)
			}
		}
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating %s: %v\n", *outPath, err)
			os.Exit(2)
		}
		if err := render.HTML(f, rep); err != nil {
			_ = f.Close()
			fmt.Fprintf(os.Stderr, "error rendering HTML report: %v\n", err)
			os.Exit(2)
		}
		_ = f.Close()
		fmt.Printf("Demo HTML report written to %s\n", *outPath)
	}
}
