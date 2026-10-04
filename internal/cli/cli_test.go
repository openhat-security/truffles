package cli

import (
	"flag"
	"strings"
	"testing"
)

func TestParseFlagsAllowsFlagsAfterPositionals(t *testing.T) {
	// Go's flag package stops at the first non-flag argument, so
	// `search torvalds lin* -no-proxy` would silently drop -no-proxy.
	fs := newFlagSet("test")
	noProxy := fs.Bool("no-proxy", false, "")
	limit := fs.Int("limit", 0, "")

	err := parseFlags(fs, []string{"torvalds", "lin*", "-no-proxy", "-limit", "5"})
	if err != nil {
		t.Fatal(err)
	}
	if !*noProxy {
		t.Error("-no-proxy after positionals was dropped")
	}
	if *limit != 5 {
		t.Errorf("-limit = %d, want 5", *limit)
	}
}

func TestParseFlagsKeepsFlagValuesWithTheirFlags(t *testing.T) {
	fs := newFlagSet("test")
	q := fs.String("q", "", "")

	err := parseFlags(fs, []string{"-q", "-not-a-flag", "pattern"})
	if err != nil {
		t.Fatal(err)
	}
	if *q != "-not-a-flag" {
		t.Errorf("-q = %q, want the value kept even though it looks like a flag", *q)
	}
}

func TestParseFlagsHandlesDoubleDash(t *testing.T) {
	fs := newFlagSet("test")
	verbose := fs.Bool("v", false, "")
	fs.Usage = func() {}

	// Everything after "--" is positional, even if it looks like a flag.
	if err := parseFlags(fs, []string{"-v", "--", "-not-a-flag"}); err != nil && err != flag.ErrHelp {
		t.Fatal(err)
	}
	if !*verbose {
		t.Error("-v before -- was dropped")
	}
}

func TestMainHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}, {"-help"}} {
		if err := Main(args); err != nil {
			t.Errorf("Main(%v) = %v, want nil", args, err)
		}
	}
}

func TestMainUnknownCommand(t *testing.T) {
	err := Main([]string{"bogus"})
	if err == nil {
		t.Fatal("Main(bogus) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error = %v, want it to name the unknown command", err)
	}
}

func TestMainSubcommandHelpSucceeds(t *testing.T) {
	// `truffles scan -h` must exit 0, not 1: the flag package returns
	// flag.ErrHelp, which used to be treated as a failure.
	if err := Main([]string{"scan", "-h"}); err != nil {
		t.Errorf("Main(scan -h) = %v, want nil", err)
	}
	if err := Main([]string{"search", "-h"}); err != nil {
		t.Errorf("Main(search -h) = %v, want nil", err)
	}
}
