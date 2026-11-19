package render

import (
	"io"
	"os"
)

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiCyan    = "\033[36m"
	ansiMagenta = "\033[35m"
)

// ShouldUseColor decides whether ANSI color codes should be emitted to w.
func ShouldUseColor(w io.Writer, noColorFlag bool) bool {
	if noColorFlag {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok && os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

type palette struct {
	enabled bool
}

func (p palette) paint(code, s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return code + s + ansiReset
}

func (p palette) bold(s string) string    { return p.paint(ansiBold, s) }
func (p palette) dim(s string) string     { return p.paint(ansiDim, s) }
func (p palette) green(s string) string   { return p.paint(ansiGreen, s) }
func (p palette) red(s string) string     { return p.paint(ansiRed, s) }
func (p palette) yellow(s string) string  { return p.paint(ansiYellow, s) }
func (p palette) cyan(s string) string    { return p.paint(ansiCyan, s) }
func (p palette) magenta(s string) string { return p.paint(ansiMagenta, s) }
