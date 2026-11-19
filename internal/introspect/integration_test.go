//go:build integration

package introspect

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"schema-drift/internal/dsn"
	"schema-drift/internal/model"
)

func TestLiveDatabaseMetadataAudit(t *testing.T) {
	endpoints := []struct {
		name   string
		envVar string
	}{
		{"postgres", "TEST_PG_DSN"},
		{"mysql", "TEST_MYSQL_DSN"},
		{"mssql", "TEST_MSSQL_DSN"},
	}

	anyRun := false
	for _, ep := range endpoints {
		raw := os.Getenv(ep.envVar)
		if raw == "" {
			continue
		}
		anyRun = true
		t.Run(ep.name, func(t *testing.T) {
			parsed, err := dsn.Parse(raw)
			if err != nil {
				t.Fatalf("dsn.Parse: %v", err)
			}

			var mu sync.Mutex
			var queries []string

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			intr, closer, err := Open(ctx, parsed, Options{
				Observer: func(engine model.Engine, q string) {
					mu.Lock()
					defer mu.Unlock()
					queries = append(queries, q)
				},
			})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer closer.Close()

			tables, err := intr.Tables(ctx)
			if err != nil {
				t.Fatalf("Tables: %v", err)
			}
			if len(tables) == 0 {
				t.Fatalf("expected non-empty tables from %s", ep.name)
			}
			for _, q := range queries {
				if !IsMetadataOnlyQuery(q) {
					t.Errorf("query audit failed; non-metadata SQL executed: %s", q)
				}
			}
		})
	}
	if !anyRun {
		t.Skip("no TEST_PG_DSN / TEST_MYSQL_DSN / TEST_MSSQL_DSN configured")
	}
}
