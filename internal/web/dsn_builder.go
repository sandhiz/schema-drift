package web

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"schema-drift/internal/dsn"
	"schema-drift/internal/model"
)

// EndpointInput represents a database connection submitted by the Web UI
// either via structured form fields or as a raw DSN string.
type EndpointInput struct {
	Mode     string `json:"mode"` // "form" or "dsn"
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"sslMode"`
	Schema   string `json:"schema"`
	DSN      string `json:"dsn"`
	Label    string `json:"label"`
}

// ResolveEndpoint converts form inputs or a raw DSN into a validated, read-only ParsedDSN.
func ResolveEndpoint(in EndpointInput, sideName string) (dsn.ParsedDSN, string, error) {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	schema := strings.TrimSpace(in.Schema)

	if mode == "dsn" || (mode == "" && strings.TrimSpace(in.DSN) != "") {
		raw := strings.TrimSpace(in.DSN)
		if raw == "" {
			return dsn.ParsedDSN{}, "", fmt.Errorf("%s: DSN is required", sideName)
		}
		parsed, err := dsn.Parse(raw)
		if err != nil {
			return dsn.ParsedDSN{}, "", fmt.Errorf("%s: %v", sideName, dsn.SanitizeError(err, in.Password))
		}
		if strings.TrimSpace(in.Label) != "" {
			parsed.DisplayLabel = strings.TrimSpace(in.Label)
		}
		return parsed, schema, nil
	}

	eng := model.Engine(strings.ToLower(strings.TrimSpace(in.Engine)))
	switch eng {
	case model.EngineMySQL, model.EnginePostgres, model.EngineMSSQL:
	case "":
		return dsn.ParsedDSN{}, "", fmt.Errorf("%s: engine is required (mysql, postgres, or mssql)", sideName)
	default:
		return dsn.ParsedDSN{}, "", fmt.Errorf("%s: unsupported engine %q", sideName, in.Engine)
	}

	host := strings.TrimSpace(in.Host)
	if host == "" {
		return dsn.ParsedDSN{}, "", fmt.Errorf("%s: host is required", sideName)
	}
	database := strings.TrimSpace(in.Database)
	if database == "" {
		return dsn.ParsedDSN{}, "", fmt.Errorf("%s: database name is required", sideName)
	}

	port := strings.TrimSpace(in.Port)
	if port == "" {
		port = defaultPort(eng)
	}

	hostPort := host
	if _, _, splitErr := net.SplitHostPort(host); splitErr != nil {
		hostPort = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}

	u := &url.URL{
		Host: hostPort,
	}

	user := strings.TrimSpace(in.User)
	if user != "" {
		if in.Password != "" {
			u.User = url.UserPassword(user, in.Password)
		} else {
			u.User = url.User(user)
		}
	}

	q := url.Values{}
	ssl := strings.ToLower(strings.TrimSpace(in.SSLMode))

	switch eng {
	case model.EngineMySQL:
		u.Scheme = "mysql"
		u.Path = "/" + database
		switch ssl {
		case "require":
			q.Set("tls", "true")
		case "skip-verify":
			q.Set("tls", "skip-verify")
		case "disable":
			q.Set("tls", "false")
		}
	case model.EnginePostgres:
		u.Scheme = "postgres"
		u.Path = "/" + database
		switch ssl {
		case "disable", "require", "verify-full":
			q.Set("sslmode", ssl)
		case "skip-verify":
			q.Set("sslmode", "require")
		}
	case model.EngineMSSQL:
		u.Scheme = "sqlserver"
		q.Set("database", database)
		switch ssl {
		case "disable":
			q.Set("encrypt", "disable")
		case "require":
			q.Set("encrypt", "true")
		case "skip-verify":
			q.Set("encrypt", "true")
			q.Set("TrustServerCertificate", "true")
		}
	}

	u.RawQuery = q.Encode()
	parsed, err := dsn.Parse(u.String())
	if err != nil {
		return dsn.ParsedDSN{}, "", fmt.Errorf("%s: %v", sideName, dsn.SanitizeError(err, in.Password))
	}
	if in.Password != "" {
		parsed.Secrets = append(parsed.Secrets, in.Password)
	}
	if strings.TrimSpace(in.Label) != "" {
		parsed.DisplayLabel = strings.TrimSpace(in.Label)
	}
	return parsed, schema, nil
}

func defaultPort(engine model.Engine) string {
	switch engine {
	case model.EngineMySQL:
		return "3306"
	case model.EnginePostgres:
		return "5432"
	case model.EngineMSSQL:
		return "1433"
	default:
		return ""
	}
}
