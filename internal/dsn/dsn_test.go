package dsn

import (
	"errors"
	"strings"
	"testing"

	"schema-drift/internal/model"
)

func TestParseEnginesAndMasking(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		wantEngine     model.Engine
		wantDriver     string
		wantLabel      string
		wantContains   string
		mustNotContain []string
	}{
		{
			name:           "postgres standard URL",
			raw:            "postgres://alice:s3cr3tPass!@db.internal:5432/payments?sslmode=require",
			wantEngine:     model.EnginePostgres,
			wantDriver:     "pgx",
			wantLabel:      "postgres://db.internal:5432/payments",
			wantContains:   "default_transaction_read_only=on",
			mustNotContain: []string{"s3cr3tPass!"},
		},
		{
			name:           "postgresql alias URL with encoded password",
			raw:            "postgresql://bob:p%40ss%2Fword@10.0.0.5:5432/core",
			wantEngine:     model.EnginePostgres,
			wantDriver:     "pgx",
			wantLabel:      "postgres://10.0.0.5:5432/core",
			wantContains:   "default_transaction_read_only=on",
			mustNotContain: []string{"p%40ss%2Fword", "p@ss/word"},
		},
		{
			name:           "mysql URL converted to go-sql-driver format",
			raw:            "mysql://root:my$up3rSecret@mysql.prod:3306/orders?tls=true",
			wantEngine:     model.EngineMySQL,
			wantDriver:     "mysql",
			wantLabel:      "mysql://mysql.prod:3306/orders",
			wantContains:   "parseTime=true",
			mustNotContain: []string{"my$up3rSecret"},
		},
		{
			name:           "mysql native go-sql-driver DSN",
			raw:            "appuser:NativePw99@tcp(127.0.0.1:3307)/inventory?charset=utf8mb4",
			wantEngine:     model.EngineMySQL,
			wantDriver:     "mysql",
			wantLabel:      "mysql://127.0.0.1:3307/inventory",
			wantContains:   "parseTime=true",
			mustNotContain: []string{"NativePw99"},
		},
		{
			name:           "sqlserver URL with database query param",
			raw:            "sqlserver://sa:Strong!Pass123@sql.corp:1433?database=LegacyERP&encrypt=disable",
			wantEngine:     model.EngineMSSQL,
			wantDriver:     "sqlserver",
			wantLabel:      "sqlserver://sql.corp:1433/LegacyERP",
			wantContains:   "ApplicationIntent=ReadOnly",
			mustNotContain: []string{"Strong!Pass123"},
		},
		{
			name:           "mssql alias URL",
			raw:            "mssql://reader:ReadOnlyPw@localhost:1433/billing?password=QueryPw",
			wantEngine:     model.EngineMSSQL,
			wantDriver:     "sqlserver",
			wantLabel:      "sqlserver://localhost:1433/billing",
			wantContains:   "ApplicationIntent=ReadOnly",
			mustNotContain: []string{"ReadOnlyPw", "QueryPw"},
		},
		{
			name:           "postgres libpq key-value DSN",
			raw:            "host=pg.local port=5432 user=ro_user password='quoted secret' dbname=analytics",
			wantEngine:     model.EnginePostgres,
			wantDriver:     "pgx",
			wantLabel:      "postgres://pg.local:5432/analytics",
			wantContains:   "default_transaction_read_only=on",
			mustNotContain: []string{"quoted secret"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := Parse(tc.raw)
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}
			if parsed.Engine != tc.wantEngine {
				t.Errorf("Engine = %q, want %q", parsed.Engine, tc.wantEngine)
			}
			if parsed.DriverName != tc.wantDriver {
				t.Errorf("DriverName = %q, want %q", parsed.DriverName, tc.wantDriver)
			}
			if parsed.DisplayLabel != tc.wantLabel {
				t.Errorf("DisplayLabel = %q, want %q", parsed.DisplayLabel, tc.wantLabel)
			}
			if !strings.Contains(parsed.DriverDSN, tc.wantContains) {
				t.Errorf("DriverDSN = %q, expected to contain %q", parsed.DriverDSN, tc.wantContains)
			}
			for _, secret := range tc.mustNotContain {
				if strings.Contains(parsed.Masked, secret) {
					t.Errorf("Masked DSN leaked secret %q: %s", secret, parsed.Masked)
				}
				if strings.Contains(parsed.DisplayLabel, secret) {
					t.Errorf("DisplayLabel leaked secret %q: %s", secret, parsed.DisplayLabel)
				}
			}
			if !strings.Contains(parsed.Masked, "***") {
				t.Errorf("Masked DSN should contain '***': %s", parsed.Masked)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"sqlite:///tmp/test.db",
		"postgres:///noaddr",
		"mysql:///noaddr",
		"sqlserver:///noaddr",
		"postgres://user:mySecret@:badport/db",
	}
	for _, raw := range cases {
		_, err := Parse(raw)
		if err == nil {
			t.Errorf("Parse(%q) expected error, got nil", raw)
		}
		if strings.Contains(err.Error(), "mySecret") {
			t.Errorf("Parse(%q) error leaked password: %v", raw, err)
		}
	}
}

func TestMaskFallbackAndSanitizeError(t *testing.T) {
	rawMalformed := "postgres://admin:TopSecret#123@bad host:5432/db"
	masked := Mask(rawMalformed)
	if strings.Contains(masked, "TopSecret#123") {
		t.Fatalf("Mask leaked password in malformed URL: %s", masked)
	}

	rawErr := errors.New("dial tcp: postgres://admin:TopSecret%23123@db:5432 failed (password=TopSecret#123)")
	sanitized := SanitizeError(rawErr, "TopSecret#123")
	if strings.Contains(sanitized.Error(), "TopSecret") {
		t.Fatalf("SanitizeError leaked password: %v", sanitized)
	}
	if !strings.Contains(sanitized.Error(), "***") {
		t.Fatalf("SanitizeError should contain '***': %v", sanitized)
	}
	if SanitizeError(nil, "x") != nil {
		t.Fatal("SanitizeError(nil) should return nil")
	}
}
