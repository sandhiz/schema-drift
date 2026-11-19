package dsn

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/go-sql-driver/mysql"
	"schema-drift/internal/model"
)

// ParsedDSN contains the normalized driver configuration and redacted metadata
// derived from a user-supplied database connection string.
type ParsedDSN struct {
	Engine       model.Engine
	DriverName   string
	DriverDSN    string
	Masked       string
	DisplayLabel string
	Secrets      []string
}

var (
	reURIUserPass      = regexp.MustCompile(`([a-zA-Z0-9+.-]+://[^:@/\s]+:)([^@/\s]+)(@)`)
	reURITruncatedPass = regexp.MustCompile(`([a-zA-Z0-9+.-]+://[^:@/\s]+:)([^@\s"']+)`)
	reMySQLPass        = regexp.MustCompile(`(^|/)([^:@/\s]+:)([^@(\s]+)(@(?:tcp|unix)\()`)
	reKVPassword       = regexp.MustCompile(`(?i)(\b(?:password|pwd)\s*=\s*)('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"|[^\s;&]+)`)
)

// Parse inspects a database DSN, detects the engine, enforces read-only session hints
// where supported by the driver, and extracts secrets for redaction.
func Parse(raw string) (ParsedDSN, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ParsedDSN{}, errors.New("empty database DSN")
	}

	lower := strings.ToLower(trimmed)

	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return parsePostgresURL(trimmed)
	case strings.HasPrefix(lower, "mysql://"):
		return parseMySQLURL(trimmed)
	case strings.HasPrefix(lower, "sqlserver://"), strings.HasPrefix(lower, "mssql://"):
		return parseMSSQLURL(trimmed)
	case isNativeMySQLDSN(trimmed):
		return parseMySQLNative(trimmed)
	case isLibpqDSN(trimmed):
		return parsePostgresLibpq(trimmed)
	default:
		return ParsedDSN{}, fmt.Errorf("unsupported DSN scheme or format (%q): expected postgres://, mysql://, sqlserver://, or native MySQL DSN", Mask(trimmed))
	}
}

func parsePostgresURL(raw string) (ParsedDSN, error) {
	u, rawSecrets, err := parseURLSafe(raw)
	if err != nil {
		return ParsedDSN{}, fmt.Errorf("invalid PostgreSQL DSN (%s): %v", Mask(raw), SanitizeError(err, rawSecrets...))
	}
	if u.Host == "" {
		return ParsedDSN{}, fmt.Errorf("invalid PostgreSQL DSN (%s): missing host", Mask(raw))
	}

	secrets := append(rawSecrets, collectURLSecrets(raw, u)...)
	driverURL := cloneURL(u)
	q := driverURL.Query()
	if !hasQueryKeyCaseInsensitive(q, "default_transaction_read_only") {
		q.Set("default_transaction_read_only", "on")
	}
	driverURL.RawQuery = q.Encode()

	dbName := strings.TrimPrefix(u.Path, "/")
	label := fmt.Sprintf("postgres://%s", u.Host)
	if dbName != "" {
		label += "/" + dbName
	}

	return ParsedDSN{
		Engine:       model.EnginePostgres,
		DriverName:   "pgx",
		DriverDSN:    driverURL.String(),
		Masked:       maskURL(u),
		DisplayLabel: label,
		Secrets:      dedupeSecrets(secrets),
	}, nil
}

func parsePostgresLibpq(raw string) (ParsedDSN, error) {
	fields := splitLibpqFields(raw)
	host := fields["host"]
	dbname := fields["dbname"]
	port := fields["port"]
	if host == "" && dbname == "" {
		return ParsedDSN{}, fmt.Errorf("invalid PostgreSQL key-value DSN (%s)", Mask(raw))
	}

	var secrets []string
	if pw := fields["password"]; pw != "" {
		secrets = appendUniqueSecret(secrets, pw)
	}

	driverDSN := raw
	if _, hasRO := fields["default_transaction_read_only"]; !hasRO {
		driverDSN = strings.TrimSpace(raw) + " default_transaction_read_only=on"
	}

	endpoint := host
	if endpoint == "" {
		endpoint = "localhost"
	}
	if port != "" {
		endpoint += ":" + port
	}
	label := "postgres://" + endpoint
	if dbname != "" {
		label += "/" + dbname
	}

	return ParsedDSN{
		Engine:       model.EnginePostgres,
		DriverName:   "pgx",
		DriverDSN:    driverDSN,
		Masked:       Mask(raw),
		DisplayLabel: label,
		Secrets:      secrets,
	}, nil
}

func parseMySQLURL(raw string) (ParsedDSN, error) {
	u, rawSecrets, err := parseURLSafe(raw)
	if err != nil {
		return ParsedDSN{}, fmt.Errorf("invalid MySQL DSN (%s): %v", Mask(raw), SanitizeError(err, rawSecrets...))
	}
	if u.Host == "" {
		return ParsedDSN{}, fmt.Errorf("invalid MySQL DSN (%s): missing host", Mask(raw))
	}

	secrets := dedupeSecrets(append(rawSecrets, collectURLSecrets(raw, u)...))
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = u.Host
	if !strings.Contains(cfg.Addr, ":") {
		cfg.Addr += ":3306"
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			cfg.Passwd = pw
		}
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	cfg.DBName = dbName
	cfg.ParseTime = true

	q := u.Query()
	if len(q) > 0 {
		cfg.Params = make(map[string]string, len(q))
		for k, vals := range q {
			if len(vals) > 0 {
				if strings.EqualFold(k, "parseTime") {
					cfg.ParseTime = strings.EqualFold(vals[0], "true") || vals[0] == "1"
					continue
				}
				cfg.Params[k] = vals[0]
			}
		}
	}

	label := fmt.Sprintf("mysql://%s", cfg.Addr)
	if dbName != "" {
		label += "/" + dbName
	}

	return ParsedDSN{
		Engine:       model.EngineMySQL,
		DriverName:   "mysql",
		DriverDSN:    cfg.FormatDSN(),
		Masked:       maskURL(u),
		DisplayLabel: label,
		Secrets:      secrets,
	}, nil
}

func parseMySQLNative(raw string) (ParsedDSN, error) {
	cfg, err := mysql.ParseDSN(raw)
	if err != nil {
		return ParsedDSN{}, fmt.Errorf("invalid MySQL DSN (%s): %v", Mask(raw), SanitizeError(err))
	}
	var secrets []string
	if cfg.Passwd != "" {
		secrets = appendUniqueSecret(secrets, cfg.Passwd)
	}
	cfg.ParseTime = true

	maskedCfg := *cfg
	if maskedCfg.Passwd != "" {
		maskedCfg.Passwd = "***"
	}
	if len(maskedCfg.Params) > 0 {
		cleanParams := make(map[string]string, len(maskedCfg.Params))
		for k, v := range maskedCfg.Params {
			if isSecretKey(k) && v != "" {
				cleanParams[k] = "***"
			} else {
				cleanParams[k] = v
			}
		}
		maskedCfg.Params = cleanParams
	}

	addr := cfg.Addr
	if addr == "" {
		addr = "localhost:3306"
	}
	label := "mysql://" + addr
	if cfg.DBName != "" {
		label += "/" + cfg.DBName
	}

	return ParsedDSN{
		Engine:       model.EngineMySQL,
		DriverName:   "mysql",
		DriverDSN:    cfg.FormatDSN(),
		Masked:       maskedCfg.FormatDSN(),
		DisplayLabel: label,
		Secrets:      secrets,
	}, nil
}

func parseMSSQLURL(raw string) (ParsedDSN, error) {
	normalized := raw
	if strings.HasPrefix(strings.ToLower(raw), "mssql://") {
		normalized = "sqlserver://" + raw[len("mssql://"):]
	}
	u, rawSecrets, err := parseURLSafe(normalized)
	if err != nil {
		return ParsedDSN{}, fmt.Errorf("invalid SQL Server DSN (%s): %v", Mask(raw), SanitizeError(err, rawSecrets...))
	}
	if u.Host == "" {
		return ParsedDSN{}, fmt.Errorf("invalid SQL Server DSN (%s): missing host", Mask(raw))
	}

	secrets := dedupeSecrets(append(rawSecrets, collectURLSecrets(raw, u)...))
	driverURL := cloneURL(u)
	q := driverURL.Query()
	if !hasQueryKeyCaseInsensitive(q, "ApplicationIntent") {
		q.Set("ApplicationIntent", "ReadOnly")
	}
	driverURL.RawQuery = q.Encode()

	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		dbName = getQueryValueCaseInsensitive(u.Query(), "database")
	}
	label := fmt.Sprintf("sqlserver://%s", u.Host)
	if dbName != "" {
		label += "/" + dbName
	}

	maskedURL := maskURL(u)
	if strings.HasPrefix(strings.ToLower(raw), "mssql://") && strings.HasPrefix(maskedURL, "sqlserver://") {
		maskedURL = "mssql://" + maskedURL[len("sqlserver://"):]
	}

	return ParsedDSN{
		Engine:       model.EngineMSSQL,
		DriverName:   "sqlserver",
		DriverDSN:    driverURL.String(),
		Masked:       maskedURL,
		DisplayLabel: label,
		Secrets:      secrets,
	}, nil
}

// Mask redacts all passwords from a DSN or connection string without mutating non-secret structure.
func Mask(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if strings.Contains(trimmed, "://") {
		if u, _, err := parseURLSafe(trimmed); err == nil && u.Scheme != "" {
			return maskURL(u)
		}
	}

	if isNativeMySQLDSN(trimmed) {
		if cfg, err := mysql.ParseDSN(trimmed); err == nil {
			if cfg.Passwd != "" {
				cfg.Passwd = "***"
			}
			return cfg.FormatDSN()
		}
	}

	masked := reURIUserPass.ReplaceAllString(trimmed, "${1}***${3}")
	masked = reMySQLPass.ReplaceAllString(masked, "${1}${2}***${4}")
	masked = reKVPassword.ReplaceAllString(masked, "${1}***")
	return masked
}

// SanitizeError ensures no credential string from a DSN leaks through driver or network errors.
func SanitizeError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	// Sort secrets longest-first so shorter substrings do not partially corrupt longer secrets.
	uniq := make([]string, 0, len(secrets)*2)
	for _, s := range secrets {
		if s == "" {
			continue
		}
		uniq = appendUniqueSecret(uniq, s)
		if esc := url.QueryEscape(s); esc != s {
			uniq = appendUniqueSecret(uniq, esc)
		}
		if esc := url.PathEscape(s); esc != s {
			uniq = appendUniqueSecret(uniq, esc)
		}
	}
	sort.Slice(uniq, func(i, j int) bool {
		return len(uniq[i]) > len(uniq[j])
	})

	for _, s := range uniq {
		msg = strings.ReplaceAll(msg, s, "***")
	}
	msg = reURIUserPass.ReplaceAllString(msg, "${1}***${3}")
	msg = reURITruncatedPass.ReplaceAllString(msg, "${1}***")
	msg = reKVPassword.ReplaceAllString(msg, "${1}***")
	return errors.New(msg)
}

// parseURLSafe parses database URLs gracefully even when passwords contain unencoded '#' or '@' characters.
func parseURLSafe(raw string) (*url.URL, []string, error) {
	var rawSecrets []string
	schemeIdx := strings.Index(raw, "://")
	if schemeIdx > 0 {
		scheme := raw[:schemeIdx]
		after := raw[schemeIdx+3:]
		// Locate authority boundary (first '/' or '?') without letting '#' in a password truncate userinfo.
		limit := len(after)
		if idx := strings.IndexAny(after, "/?"); idx >= 0 {
			limit = idx
		}
		if at := strings.LastIndex(after[:limit], "@"); at >= 0 {
			userinfo := after[:at]
			hostAndRest := after[at+1:]
			if colon := strings.Index(userinfo, ":"); colon >= 0 {
				rawPass := userinfo[colon+1:]
				if rawPass != "" {
					rawSecrets = appendUniqueSecret(rawSecrets, rawPass)
				}
			}
			// Try standard url.Parse first when no unencoded '#' or extra '@' sits inside userinfo.
			if !strings.ContainsAny(userinfo, "#@") {
				if u, err := url.Parse(raw); err == nil {
					return u, rawSecrets, nil
				}
			}
			u, err := url.Parse(scheme + "://" + hostAndRest)
			if err != nil {
				return nil, rawSecrets, err
			}
			if colon := strings.Index(userinfo, ":"); colon >= 0 {
				user := decodeUserPart(userinfo[:colon])
				pass := decodeUserPart(userinfo[colon+1:])
				u.User = url.UserPassword(user, pass)
				if pass != "" {
					rawSecrets = appendUniqueSecret(rawSecrets, pass)
				}
			} else if userinfo != "" {
				u.User = url.User(decodeUserPart(userinfo))
			}
			return u, rawSecrets, nil
		}
	}

	u, err := url.Parse(raw)
	return u, rawSecrets, err
}

func decodeUserPart(s string) string {
	if decoded, err := url.PathUnescape(s); err == nil {
		return decoded
	}
	return s
}

func dedupeSecrets(in []string) []string {
	var out []string
	for _, s := range in {
		out = appendUniqueSecret(out, s)
	}
	return out
}

func maskURL(u *url.URL) string {
	c := cloneURL(u)
	if c.User != nil {
		username := c.User.Username()
		if _, hasPw := c.User.Password(); hasPw {
			c.User = url.UserPassword(username, "***")
		}
	}

	q := c.Query()
	changed := false
	for key, vals := range q {
		if isSecretKey(key) && len(vals) > 0 && vals[0] != "" {
			q.Set(key, "***")
			changed = true
		}
	}
	if changed {
		c.RawQuery = q.Encode()
	}

	out := c.String()
	// url.URL.String percent-encodes '***' in userinfo; restore clean '***' for readability.
	out = strings.ReplaceAll(out, ":%2A%2A%2A@", ":***@")
	out = strings.ReplaceAll(out, "=%2A%2A%2A", "=***")
	return out
}

func cloneURL(u *url.URL) *url.URL {
	c := *u
	if u.User != nil {
		userCopy := *u.User
		c.User = &userCopy
	}
	return &c
}

func collectURLSecrets(raw string, u *url.URL) []string {
	var secrets []string
	if u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			secrets = appendUniqueSecret(secrets, pw)
		}
	}
	for k, vals := range u.Query() {
		if isSecretKey(k) {
			for _, v := range vals {
				if v != "" {
					secrets = appendUniqueSecret(secrets, v)
				}
			}
		}
	}
	// Also capture raw unparsed user:pass slice if present in the original URL.
	if idx := strings.Index(raw, "://"); idx >= 0 {
		rest := raw[idx+3:]
		if at := strings.Index(rest, "@"); at >= 0 {
			userinfo := rest[:at]
			if colon := strings.Index(userinfo, ":"); colon >= 0 {
				rawPass := userinfo[colon+1:]
				if rawPass != "" {
					secrets = appendUniqueSecret(secrets, rawPass)
				}
			}
		}
	}
	return secrets
}

func appendUniqueSecret(list []string, val string) []string {
	if val == "" {
		return list
	}
	for _, existing := range list {
		if existing == val {
			return list
		}
	}
	return append(list, val)
}

func isSecretKey(k string) bool {
	return strings.EqualFold(k, "password") || strings.EqualFold(k, "pwd")
}

func hasQueryKeyCaseInsensitive(q url.Values, target string) bool {
	for k := range q {
		if strings.EqualFold(k, target) {
			return true
		}
	}
	return false
}

func getQueryValueCaseInsensitive(q url.Values, target string) string {
	for k, vals := range q {
		if strings.EqualFold(k, target) && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

func isNativeMySQLDSN(raw string) bool {
	if strings.Contains(raw, "://") {
		return false
	}
	if strings.Contains(raw, "@tcp(") || strings.Contains(raw, "@unix(") {
		return true
	}
	cfg, err := mysql.ParseDSN(raw)
	return err == nil && (cfg.Addr != "" || cfg.DBName != "") && strings.Contains(raw, "/")
}

func isLibpqDSN(raw string) bool {
	if strings.Contains(raw, "://") {
		return false
	}
	fields := splitLibpqFields(raw)
	_, hasHost := fields["host"]
	_, hasDB := fields["dbname"]
	return hasHost || hasDB
}

func splitLibpqFields(raw string) map[string]string {
	out := make(map[string]string)
	i := 0
	n := len(raw)
	for i < n {
		for i < n && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
			i++
		}
		if i >= n {
			break
		}
		keyStart := i
		for i < n && raw[i] != '=' && raw[i] != ' ' {
			i++
		}
		if i >= n || raw[i] != '=' {
			for i < n && raw[i] != ' ' {
				i++
			}
			continue
		}
		key := strings.ToLower(strings.TrimSpace(raw[keyStart:i]))
		i++ // skip '='
		if i >= n {
			out[key] = ""
			break
		}
		var val string
		if raw[i] == '\'' || raw[i] == '"' {
			quote := raw[i]
			i++
			var sb strings.Builder
			for i < n {
				if raw[i] == '\\' && i+1 < n {
					sb.WriteByte(raw[i+1])
					i += 2
					continue
				}
				if raw[i] == quote {
					i++
					break
				}
				sb.WriteByte(raw[i])
				i++
			}
			val = sb.String()
		} else {
			valStart := i
			for i < n && raw[i] != ' ' && raw[i] != '\t' {
				i++
			}
			val = raw[valStart:i]
		}
		if key != "" {
			out[key] = val
		}
	}
	return out
}
