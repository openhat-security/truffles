package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
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

func (p palette) bold(s string) string     { return p.wrap("1", s) }
func (p palette) dim(s string) string      { return p.wrap("2", s) }
func (p palette) red(s string) string      { return p.wrap("31", s) }
func (p palette) green(s string) string    { return p.wrap("32", s) }
func (p palette) yellow(s string) string   { return p.wrap("33", s) }
func (p palette) blue(s string) string     { return p.wrap("34", s) }
func (p palette) magenta(s string) string  { return p.wrap("35", s) }
func (p palette) cyan(s string) string     { return p.wrap("36", s) }
func (p palette) bgreen(s string) string   { return p.wrap("1;32", s) }
func (p palette) byellow(s string) string  { return p.wrap("1;33", s) }
func (p palette) bcyan(s string) string    { return p.wrap("1;36", s) }
func (p palette) bmagenta(s string) string { return p.wrap("1;35", s) }
func (p palette) bred(s string) string     { return p.wrap("1;31", s) }

// FormatError colors an error for stderr/pretty output.
func (p palette) FormatError(msg string) string {
	return p.red(msg)
}

// heading prints a bold cyan section heading.
func (p palette) heading(w io.Writer, title string) {
	fmt.Fprintln(w, p.bcyan(title))
	fmt.Fprintln(w)
}

// section prints a bold yellow section heading.
func (p palette) section(w io.Writer, title string) {
	fmt.Fprintln(w, p.byellow(title))
}

// helpSection prints a colored group header for help.
func (p palette) helpSection(w io.Writer, title string) {
	fmt.Fprintln(w, p.byellow(title))
}

// helpCmd prints one help row: cyan command + dim description (aligned).
func (p palette) helpCmd(w io.Writer, cmd, desc string) {
	p.helpCmdIndent(w, 0, cmd, desc)
}

func (p palette) helpCmdIndent(w io.Writer, nest int, cmd, desc string) {
	const cmdWidth = 26
	pad := strings.Repeat("  ", nest+1)
	col := padRight(cmd, cmdWidth)
	if utf8.RuneCountInString(cmd) >= cmdWidth {
		col = cmd + "  "
	}
	fmt.Fprintf(w, "%s%s%s\n", pad, p.cyan(col), p.dim(desc))
}

// helpFlag prints a flag row (same alignment as helpCmd).
func (p palette) helpFlag(w io.Writer, flag, desc string) {
	p.helpCmd(w, flag, desc)
}

// helpUsage prints the dim "usage:" line + cyan binary name.
func (p palette) helpUsage(w io.Writer, synopsis string) {
	fmt.Fprintln(w, p.dim("usage:"))
	fmt.Fprintf(w, "  %s\n", p.cyan(synopsis))
	fmt.Fprintln(w)
}

func (p palette) printKV(w io.Writer, label, value string) {
	fmt.Fprintf(w, "  %s  %s\n", p.dim(padRight(label, 9)), value)
}

var usdRe = regexp.MustCompile(`\$[0-9]+(?:\.[0-9]+)?`)

func (p palette) highlightUSD(s string) string {
	if !p.on || !strings.Contains(s, "$") {
		return s
	}
	return usdRe.ReplaceAllStringFunc(s, p.green)
}

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

func padRight(s string, n int) string {
	if n <= 0 {
		return s
	}
	w := utf8.RuneCountInString(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func (p palette) printBanner(w io.Writer) {
	fmt.Fprintln(w, p.bcyan("truffles"))
}

func (p palette) printTagline(w io.Writer) {
	fmt.Fprintln(w, p.dim("Find GitHub repos at scale, then scan for secrets with trufflehog."))
	fmt.Fprintln(w)
}
