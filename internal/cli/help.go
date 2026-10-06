package cli

import (
	"fmt"
	"io"
)

type helpEntry struct {
	Cmd  string
	What string
}

func (p palette) printStyledUsage(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, p.dim("usage:"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles search"), p.dim("[flags] [pattern]..."))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles scan"), p.dim("[flags]"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles wizard"), p.dim("guided walkthrough"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles playbook"), p.dim("-f <playbook.yaml> [-d]"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles examples"), p.dim("usage examples"))
	fmt.Fprintf(w, "  %s\n", p.cyan("truffles help"))
	fmt.Fprintln(w)
}

func (p palette) printFullHelp(w io.Writer) {
	p.printStyledUsage(w)
	fmt.Fprintln(w, p.dim("Full command reference."))
	fmt.Fprintln(w)

	p.helpSection(w, "search")
	p.helpCmd(w, "search [pattern]...", "Find repos by owner, patterns, or globally")
	p.helpCmd(w, "-owner <user/org>", "Enumerate repos for owner(s) (comma-separated)")
	p.helpCmd(w, "-q <query>", "GitHub search query (repeatable, global mode)")
	p.helpCmd(w, "-filter <glob>", "Extra local filter (comma-separated, global mode)")
	p.helpCmd(w, "-regex", "Treat patterns as regex instead of globs")
	p.helpCmd(w, "-limit <n>", "Max results (0=unlimited, max 10000)")
	p.helpCmd(w, "-out <file>", "Write results to file (default stdout)")
	p.helpCmd(w, "-workers <n>", "Concurrent page fetches (default 8)")
	p.helpCmd(w, "-pool-size <n>", "Target validated proxies (default 30)")
	p.helpCmd(w, "-pool-wait <dur>", "Wait for first proxy (default 60s)")
	p.helpCmd(w, "-no-proxy", "Skip proxy pool, connect directly")
	p.helpCmd(w, "-v", "Verbose: show proxy for each request and probe failures")
	fmt.Fprintln(w)

	p.helpSection(w, "scan")
	p.helpCmd(w, "scan", "Scan repos from a file with trufflehog")
	p.helpCmd(w, "-f, -file <file>", "File with GitHub URLs (default repos.txt)")
	p.helpCmd(w, "-bin <path>", "trufflehog binary (default trufflehog)")
	p.helpCmd(w, "-w, -workers <n>", "Concurrent repos (default 4)")
	p.helpCmd(w, "-token <t>", "GitHub token for private repos")
	p.helpCmd(w, "-format <f>", "pretty | csv | jsonl (default pretty)")
	p.helpCmd(w, "-out <file>", "- for stdout or <name>-<ts>.<ext>")
	p.helpCmd(w, "-json", "Use trufflehog JSON (default true)")
	p.helpCmd(w, "-no-verification", "Skip live verification (unverified only)")
	p.helpCmd(w, "-max-depth <n>", "Last N commits (0=all history; lossy)")
	p.helpCmd(w, "-exclude-paths <s>", "Skip paths/globs (comma-separated)")
	p.helpCmd(w, "-color <m>", "auto|always|never (default auto)")
	p.helpCmd(w, "-v", "Show trufflehog logs")
	fmt.Fprintln(w)

	p.helpSection(w, "general")
	p.helpCmd(w, "wizard", "Interactive guided setup: search + scan")
	p.helpCmd(w, "examples", "Show usage examples")
	p.helpCmd(w, "playbook", "Run playbook from YAML (-f playbook.yaml [-d] [-pidfile])")
	p.helpCmd(w, "help, -h, --help", "Show this help")
	p.helpCmd(w, "NO_COLOR", "Disable color if set")
	fmt.Fprintln(w)
}
