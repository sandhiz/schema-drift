package web

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"schema-drift/internal/dsn"
)

const maxLogEntries = 120

// LogEntry represents one structured runtime log event emitted by the Go server.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Scope     string `json:"scope"`
	Message   string `json:"message"`
}

// Logger writes NestJS-style live logs to an io.Writer and retains recent
// sanitized entries in an in-memory ring buffer for the Web UI console drawer.
type Logger struct {
	mu      sync.Mutex
	out     io.Writer
	color   bool
	entries []LogEntry
}

// NewLogger creates a thread-safe server logger. Passing nil out disables terminal printing
// while still retaining sanitized entries in memory.
func NewLogger(out io.Writer, color bool) *Logger {
	return &Logger{
		out:     out,
		color:   color,
		entries: make([]LogEntry, 0, maxLogEntries),
	}
}

// Info logs a standard operational event.
func (l *Logger) Info(scope, format string, args ...any) {
	l.log("INFO", scope, fmt.Sprintf(format, args...))
}

// OK logs a successful completion event.
func (l *Logger) OK(scope, format string, args ...any) {
	l.log("OK", scope, fmt.Sprintf(format, args...))
}

// Warn logs a non-fatal warning or detected schema drift summary.
func (l *Logger) Warn(scope, format string, args ...any) {
	l.log("WARN", scope, fmt.Sprintf(format, args...))
}

// Error logs a sanitized error message without exposing credentials.
func (l *Logger) Error(scope string, msg string, secrets ...string) {
	l.log("ERROR", scope, msg, secrets...)
}

// SQL logs a compact single-line metadata catalog query.
func (l *Logger) SQL(scope, query string) {
	compact := strings.Join(strings.Fields(query), " ")
	l.log("SQL", scope, compact)
}

// Entries returns a snapshot copy of recent log entries.
func (l *Logger) Entries() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LogEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Clear empties the in-memory log buffer.
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = l.entries[:0]
}

func (l *Logger) log(level, scope, rawMsg string, secrets ...string) {
	clean := dsn.Mask(rawMsg)
	if len(secrets) > 0 {
		clean = dsn.SanitizeError(errors.New(clean), secrets...).Error()
	}

	ts := time.Now().Format("15:04:05")
	entry := LogEntry{
		Timestamp: ts,
		Level:     level,
		Scope:     scope,
		Message:   clean,
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) >= maxLogEntries {
		copy(l.entries, l.entries[1:])
		l.entries[len(l.entries)-1] = entry
	} else {
		l.entries = append(l.entries, entry)
	}

	if l.out == nil {
		return
	}
	_, _ = fmt.Fprintln(l.out, formatTerminalLogLine(entry, l.color))
}

func formatTerminalLogLine(e LogEntry, color bool) string {
	lvlPadded := fmt.Sprintf("%-5s", e.Level)
	scopeTag := fmt.Sprintf("[%-10s]", e.Scope)

	if !color {
		return fmt.Sprintf("[schema-drift] %s  %s  %s %s", e.Timestamp, lvlPadded, scopeTag, e.Message)
	}

	const (
		reset   = "\033[0m"
		dim     = "\033[2m"
		green   = "\033[32m"
		cyan    = "\033[36m"
		yellow  = "\033[33m"
		red     = "\033[31m"
		magenta = "\033[35m"
	)

	lvlColor := cyan
	switch e.Level {
	case "OK":
		lvlColor = green
	case "WARN":
		lvlColor = yellow
	case "ERROR":
		lvlColor = red
	case "SQL":
		lvlColor = magenta
	}

	return fmt.Sprintf("%s[schema-drift]%s %s%s%s  %s%s%s  %s%s%s %s",
		green, reset,
		dim, e.Timestamp, reset,
		lvlColor, lvlPadded, reset,
		yellow, scopeTag, reset,
		e.Message,
	)
}
