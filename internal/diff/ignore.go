package diff

import (
	"fmt"
	"path"
	"strings"

	"schema-drift/internal/model"
)

// IgnoreMatcher evaluates table names and schema-qualified keys against glob patterns.
type IgnoreMatcher struct {
	patterns []string
}

// NewIgnoreMatcher parses comma-separated or repeated glob patterns (e.g. "temp_*,audit_*").
// Pattern syntax follows path.Match semantics and is evaluated case-insensitively.
func NewIgnoreMatcher(rawPatterns []string) (*IgnoreMatcher, error) {
	var parsed []string
	seen := make(map[string]struct{})
	for _, entry := range rawPatterns {
		for _, part := range strings.Split(entry, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			lower := strings.ToLower(p)
			if _, err := path.Match(lower, "sample_table"); err != nil {
				return nil, fmt.Errorf("invalid --ignore glob pattern %q: %w", p, err)
			}
			if _, ok := seen[lower]; !ok {
				seen[lower] = struct{}{}
				parsed = append(parsed, lower)
			}
		}
	}
	return &IgnoreMatcher{patterns: parsed}, nil
}

// Patterns returns the deduplicated glob patterns in lowercased form.
func (m *IgnoreMatcher) Patterns() []string {
	if m == nil || len(m.patterns) == 0 {
		return nil
	}
	out := make([]string, len(m.patterns))
	copy(out, m.patterns)
	return out
}

// Match returns true when the table matches any configured glob pattern
// by table name, logical key, or schema-qualified name.
func (m *IgnoreMatcher) Match(tbl model.Table) bool {
	if m == nil || len(m.patterns) == 0 {
		return false
	}

	candidates := make([]string, 0, 3)
	if tbl.Name != "" {
		candidates = append(candidates, strings.ToLower(tbl.Name))
	}
	if tbl.LogicalKey != "" {
		candidates = append(candidates, strings.ToLower(tbl.LogicalKey))
	}
	if tbl.Schema != "" && tbl.Name != "" {
		candidates = append(candidates, strings.ToLower(tbl.Schema+"."+tbl.Name))
	}

	for _, pattern := range m.patterns {
		for _, cand := range candidates {
			if matched, err := path.Match(pattern, cand); err == nil && matched {
				return true
			}
		}
	}
	return false
}
