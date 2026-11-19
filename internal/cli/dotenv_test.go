package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	input := strings.Join([]string{
		"\uFEFF# Leading comment with UTF-8 BOM",
		"",
		"   # Indented comment",
		"export PORT=3000 # inline comment",
		"SCHEMA_DRIFT_A=mysql://root:p#1@localhost:3306/db # keep password hash",
		`DOUBLE_QUOTED="line1\n\"quoted\"\\#keep" # trailing comment`,
		`SINGLE_QUOTED='single#keep\nliteral'`,
		"EMPTY=",
		"INVALID_NO_EQUALS",
	}, "\r\n")

	got, err := parseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseDotEnv error: %v", err)
	}

	wants := map[string]string{
		"PORT":           "3000",
		"SCHEMA_DRIFT_A": "mysql://root:p#1@localhost:3306/db",
		"DOUBLE_QUOTED":  "line1\n\"quoted\"\\#keep",
		"SINGLE_QUOTED":  `single#keep\nliteral`,
		"EMPTY":          "",
	}

	if len(got) != len(wants) {
		t.Fatalf("parsed %d keys (%+v), want %d (%+v)", len(got), got, len(wants), wants)
	}
	for k, want := range wants {
		if got[k] != want {
			t.Errorf("got[%q] = %q, want %q", k, got[k], want)
		}
	}
}

func TestLoadDotEnvWith(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.env")
	secondPath := filepath.Join(dir, "local.env")

	// Missing files return empty path and nil error.
	envStore := map[string]string{}
	lookup := func(k string) (string, bool) {
		v, ok := envStore[k]
		return v, ok
	}
	setenv := func(k, v string) error {
		envStore[k] = v
		return nil
	}

	loaded, err := loadDotEnvWith([]string{missingPath}, lookup, setenv)
	if err != nil {
		t.Fatalf("loadDotEnvWith(missing) error: %v", err)
	}
	if loaded != "" {
		t.Fatalf("loadDotEnvWith(missing) = %q, want empty", loaded)
	}

	// Write second candidate; pre-existing env var must NOT be overwritten.
	content := "PORT=3000\nSCHEMA_DRIFT_ADDR=127.0.0.1:4500\n"
	if err := os.WriteFile(secondPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	envStore["PORT"] = "9999"

	loaded, err = loadDotEnvWith([]string{missingPath, secondPath}, lookup, setenv)
	if err != nil {
		t.Fatalf("loadDotEnvWith(fallback) error: %v", err)
	}
	if loaded != secondPath {
		t.Fatalf("loaded = %q, want %q", loaded, secondPath)
	}
	if envStore["PORT"] != "9999" {
		t.Errorf("existing PORT env overwritten: got %q, want 9999", envStore["PORT"])
	}
	if envStore["SCHEMA_DRIFT_ADDR"] != "127.0.0.1:4500" {
		t.Errorf("SCHEMA_DRIFT_ADDR = %q, want 127.0.0.1:4500", envStore["SCHEMA_DRIFT_ADDR"])
	}
}
