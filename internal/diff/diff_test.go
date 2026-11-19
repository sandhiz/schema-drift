package diff

import (
	"testing"

	"schema-drift/internal/model"
)

func TestIgnoreMatcher(t *testing.T) {
	m, err := NewIgnoreMatcher([]string{"temp_*, audit_*", "*_bak"})
	if err != nil {
		t.Fatalf("NewIgnoreMatcher error: %v", err)
	}
	if len(m.Patterns()) != 3 {
		t.Fatalf("expected 3 patterns, got %v", m.Patterns())
	}

	ignored := []model.Table{
		{Schema: "dbo", Name: "temp_users", LogicalKey: "temp_users"},
		{Schema: "public", Name: "TEMP_ORDERS", LogicalKey: "TEMP_ORDERS"},
		{Schema: "sales", Name: "audit_events", LogicalKey: "sales.audit_events"},
		{Schema: "public", Name: "legacy_bak", LogicalKey: "legacy_bak"},
	}
	for _, tbl := range ignored {
		if !m.Match(tbl) {
			t.Errorf("expected table %+v to match ignore pattern", tbl)
		}
	}

	kept := []model.Table{
		{Schema: "dbo", Name: "users", LogicalKey: "users"},
		{Schema: "public", Name: "temporary_fix", LogicalKey: "temporary_fix"},
	}
	for _, tbl := range kept {
		if m.Match(tbl) {
			t.Errorf("did not expect table %+v to match ignore pattern", tbl)
		}
	}

	if _, err := NewIgnoreMatcher([]string{"bad_[pattern"}); err == nil {
		t.Fatal("expected error for malformed glob pattern")
	}
}

func TestCompareInvalidIgnorePattern(t *testing.T) {
	_, err := Compare(model.SchemaSnapshot{}, model.SchemaSnapshot{}, Options{
		IgnorePatterns: []string{"[unclosed"},
	})
	if err == nil {
		t.Fatal("expected error for invalid glob pattern")
	}
}

func TestReportHelpersAndCaseCollisions(t *testing.T) {
	snapA := model.SchemaSnapshot{
		Engine: model.EnginePostgres,
		Tables: []model.Table{
			{
				Schema:     "public",
				LogicalKey: "Users",
				Name:       "Users",
				Columns: []model.Column{
					{Name: "ID", Ordinal: 1, CanonicalType: "bigint"},
					{Name: "id", Ordinal: 2, CanonicalType: "integer"},
				},
			},
			{
				Schema:     "public",
				LogicalKey: "users",
				Name:       "users",
				Columns: []model.Column{
					{Name: "id", Ordinal: 1, CanonicalType: "bigint"},
				},
			},
		},
	}
	snapB := model.SchemaSnapshot{
		Engine: model.EnginePostgres,
		Tables: []model.Table{
			{
				Schema:     "public",
				LogicalKey: "users",
				Name:       "users",
				Columns: []model.Column{
					{Name: "id", Ordinal: 1, CanonicalType: "bigint"},
				},
			},
		},
	}

	rep, err := Compare(snapA, snapB, Options{})
	if err != nil {
		t.Fatalf("Compare() error: %v", err)
	}
	if !rep.Summary.HasDrift {
		t.Fatal("expected drift due to extra case-colliding table Users")
	}
	if len(rep.TablesByStatus(StatusOnlyA)) != 1 {
		t.Errorf("expected 1 table OnlyA, got %d", len(rep.TablesByStatus(StatusOnlyA)))
	}
	if len(rep.TablesByStatus(StatusIdentical)) != 1 {
		t.Errorf("expected 1 identical table, got %d", len(rep.TablesByStatus(StatusIdentical)))
	}
	if rep.Summary.TotalTableDiffs() != 1 {
		t.Errorf("TotalTableDiffs = %d, want 1", rep.Summary.TotalTableDiffs())
	}
}
