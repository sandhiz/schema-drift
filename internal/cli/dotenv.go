package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	loadedDotEnvMu   sync.RWMutex
	loadedDotEnvPath string
)

// LoadedDotEnvPath returns the path of the .env file auto-loaded at startup, if any.
func LoadedDotEnvPath() string {
	loadedDotEnvMu.RLock()
	defer loadedDotEnvMu.RUnlock()
	return loadedDotEnvPath
}

// LoadDefaultDotEnv loads environment variables from a local .env file if present,
// preferring .env in the current working directory and falling back to the executable directory.
// Missing files are ignored; existing OS environment variables are never overwritten.
func LoadDefaultDotEnv() (string, error) {
	candidates := []string{".env"}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if wd, wdErr := os.Getwd(); wdErr != nil || !strings.EqualFold(filepath.Clean(exeDir), filepath.Clean(wd)) {
			candidates = append(candidates, filepath.Join(exeDir, ".env"))
		}
	}

	loaded, err := LoadDotEnv(candidates...)
	loadedDotEnvMu.Lock()
	loadedDotEnvPath = loaded
	loadedDotEnvMu.Unlock()
	return loaded, err
}

// LoadDotEnv loads variables from the first existing .env file in paths without
// overwriting variables already set in the process environment.
func LoadDotEnv(paths ...string) (string, error) {
	return loadDotEnvWith(paths, os.LookupEnv, os.Setenv)
}

func loadDotEnvWith(paths []string, lookup func(string) (string, bool), setenv func(string, string) error) (string, error) {
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", fmt.Errorf("open %s: %w", p, err)
		}
		vals, parseErr := parseDotEnv(f)
		_ = f.Close()
		if parseErr != nil {
			return "", fmt.Errorf("parse %s: %w", p, parseErr)
		}

		for k, v := range vals {
			if _, exists := lookup(k); exists {
				continue
			}
			if err := setenv(k, v); err != nil {
				return "", fmt.Errorf("set %s from %s: %w", k, p, err)
			}
		}
		return p, nil
	}
	return "", nil
}

func parseDotEnv(r io.Reader) (map[string]string, error) {
	vals := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()
		if lineNum == 0 {
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		lineNum++

		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "export ") || strings.HasPrefix(line, "export\t") {
			line = strings.TrimSpace(line[len("export"):])
		}

		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:eq])
		if key == "" {
			continue
		}
		vals[key] = parseDotEnvValue(line[eq+1:])
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return vals, nil
}

func parseDotEnvValue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	switch trimmed[0] {
	case '"':
		return parseDoubleQuotedValue(trimmed[1:])
	case '\'':
		if end := strings.IndexByte(trimmed[1:], '\''); end >= 0 {
			return trimmed[1 : 1+end]
		}
		return trimmed[1:]
	default:
		for i := 1; i < len(trimmed); i++ {
			if trimmed[i] == '#' && (trimmed[i-1] == ' ' || trimmed[i-1] == '\t') {
				trimmed = trimmed[:i]
				break
			}
		}
		return strings.TrimSpace(trimmed)
	}
}

func parseDoubleQuotedValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			return b.String()
		}
		if c == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
