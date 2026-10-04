package cli

import (
	"os"
	"strings"
)

// palette provides ANSI colouring, disabled when the destination is not a
// terminal so redirected files and piped output stay clean.
type palette struct{ on bool }

func (p palette) wrap(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p palette) bold(s string) string { return p.wrap("1", s) }

func (p palette) dim(s string) string { return p.wrap("2", s) }

func (p palette) red(s string) string { return p.wrap("31", s) }

func (p palette) green(s string) string { return p.wrap("32", s) }

func (p palette) yellow(s string) string { return p.wrap("33", s) }

func (p palette) blue(s string) string { return p.wrap("34", s) }

func (p palette) magenta(s string) string { return p.wrap("35", s) }

func (p palette) cyan(s string) string { return p.wrap("36", s) }

func (p palette) bgreen(s string) string { return p.wrap("1;32", s) }

func (p palette) byellow(s string) string { return p.wrap("1;33", s) }

func (p palette) bcyan(s string) string { return p.wrap("1;36", s) }

func (p palette) bmagenta(s string) string { return p.wrap("1;35", s) }

// isTTY reports whether f is an interactive terminal.
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// resolveColor honours NO_COLOR and an explicit -color setting.
func resolveColor(mode string, f *os.File) bool {
	switch strings.ToLower(mode) {
	case "always":
		return true
	case "never":
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTTY(f)
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
