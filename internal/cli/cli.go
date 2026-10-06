package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const usage = `truffles - GitHub repo enumeration + secret scanning

usage:
  truffles search [flags] [pattern]...   find repos by owner, by pattern, or both
  truffles scan [flags]                  scan repos from a file with trufflehog
  truffles help                          show this message

Owner and pattern are each optional, but at least one is required.

WITH -owner (enumeration):
  -owner alone         -> every repo that user/org owns
  -owner + patterns    -> that user's repos, filtered by the patterns
  A repo is kept if it matches ANY pattern (union).

WITHOUT -owner (search):
  positionals are SEARCH QUERIES, not filters. Each query is fetched
  concurrently by a different worker, each through a different proxy.
  GitHub already filters, so no local filter is applied unless you pass
  -filter. Example:
    truffles search llm '*llm*'    -> two concurrent queries: "llm", "*llm*"

search flags:
  -owner string   GitHub user/org (repeatable, comma-separated). Omit to search globally
  -regex          treat patterns as regexes instead of globs
  -q string       GitHub search query (repeatable). Without -owner, positionals are used instead
  -filter string  extra local glob/regex filter in global mode (comma-separated)
  -v              verbose: log every probe failure reason and every request's proxy
  -out string     write results to file (default: stdout)
  -limit int      max results to output, max 10000 (0 = no limit)
  -workers int    concurrent page fetches (default 8)
  -pool-size int  target number of validated proxies (default 30)
  -use-direct     also spend this machine's own rate-limit budget (default true)
  -no-direct      never use this machine's own IP, proxies only
  -probe-par int  concurrent proxy validation probes (default 300)
  -pool-wait dur  how long to wait for the first working proxy (default 60s)
  -retries int    attempts per page before a retry round (default 4)
  -progress dur   log progress this often; 0 disables (default 10s)
  -no-proxy       connect directly, skipping the proxy pool
  -token string   GitHub token (raises rate limits)

proxy pool:
  Free proxies are ~4% yield and ~58% die within 45s, so the pool validates
  candidates against api.github.com in the background and replenishes itself.
  Validation yield is flat with concurrency but wall time is not, so probes
  run wide (300) by default: 400 candidates take ~13s at 300 vs ~39s at 60.
  Each proxy is paced to its own per-IP GitHub limit (60/hr core, 10/min
  search), which is what spreads the rate limit across many IPs. If the pool
  can't produce a proxy, the run falls back to a direct connection and says so.
  Pass -no-proxy to skip it, or -pool-wait 0 to fall back immediately.

scan flags:
  -f, -file string   file with GitHub URLs, one per line (default "repos.txt")
  -bin string        trufflehog binary (default "trufflehog")
  -w, -workers int   concurrent repos (default 4)
  -token string      GitHub token for private repos
  -results string    verified,unknown,unverified (default "verified,unknown")
  -json              use trufflehog JSON output (default true)
  -no-verification   skip live verification (see "speeding up scans")
  -max-depth int     only scan the last N commits per repo (0 = all history)
  -exclude-paths s   comma-separated paths/globs for trufflehog to skip
  -out string        report file (default <input>-<unixtimestamp>.txt, "-" for stdout)
  -format string     report format: pretty, csv, jsonl (default "pretty")
  -color string      colourise: auto, always, never (default "auto")
  -progress dur      log progress this often; 0 disables (default 10s)
  -v                 also show trufflehog's own log output

scan output:
  Progress goes to stderr. The report goes to -out, which defaults to a fresh
  timestamped file so runs never overwrite each other, and is flushed and
  fsynced after every repo — Ctrl-C keeps everything finished so far. Each
  repo also prints a status line, plus a heartbeat with counts and an ETA:

    [ok] stripe/ai                                       5.072s  clean
    [++] symfony/ai                                     18.483s  24 finding(s)
    [*] 10/12 done, 0 failed, 2 in flight, 28 finding(s), 1m15s elapsed, ~15s left

  Repos with findings are also printed live, in colour, on the terminal.

  -format pretty  human-readable sections with VERIFIED/UNVERIFIED badges,
                  file:line, commit, and a summary with per-detector counts
  -format csv     one row per finding: repo,verified,detector,decoder,file,
                  line,commit,timestamp,author,secret,verification_error
  -format jsonl   one JSON object per finding (JSON Lines, so the file stays
                  valid after every repo)

  csv and jsonl contain findings only — clean/failed repos appear in the
  stderr summary instead, because prose would break parsing. Colour is
  auto-detected, so redirected files stay plain; NO_COLOR is honoured.

  A repo that takes minutes is normal: trufflehog clones the full history.

speeding up scans:
  Almost all of a scan's wall time is the full-history mirror clone,
  not the secret scanning. Measured on this machine (14 cores):

    git clone --mirror streamlit/streamlit    53.9s, 1.6 GB on disk
    -max-depth 1 on 12 repos                 5m22s  (scanning is ~free)
    the same 12 repos at -w 12               5m20s  (no gain)

  Scans are network- and disk-bound, so more workers mostly multiply peak disk
  usage rather than speed things up. -workers does help when repos are small
  enough that the clone is latency- rather than bandwidth-bound.

  What actually changes the runtime, all measured:

    -max-depth N     Cuts CPU ~4x (vercel/ai: 122s -> 33s of user time) but
                     is LOSSY: -max-depth 200 found 0 of that repo's 7
                     findings, because the secrets are in old commits. Fine
                     for "is this repo dirty right now", wrong if you care
                     about historical secrets.
    -no-verification NOT a speedup: 142s vs 126s baseline, identical CPU.
                     Worth using to avoid outbound calls to third-party APIs,
                     at the cost of every result being UNVERIFIED.
    -exclude-paths   Safe on repos vendoring huge trees, e.g.
                     node_modules,vendor,dist,*.lock,*.min.js

  There is no flag for a shallow or treeless clone, so the full-history mirror
  is unavoidable in trufflehog git mode. Scanning only what is checked out
  means giving up history (and the old secrets in it).

  Failed repos are reported as [!!] with the reason trufflehog gave, and never
  counted as clean -- an unclonable repo would otherwise look like a repo with
  no secrets.

examples:
  truffles search -owner BurntSushi -workers 8 -out repos.txt
  truffles search -owner BurntSushi,torvalds,rust-lang '*llm*' -out repos.txt
  truffles search -owner BurntSushi -regex '^ri' -out repos.txt
  truffles search 'llm' -limit 100 -pool-size 40          # global
  truffles search -no-proxy -owner BurntSushi '*llm*'     # skip the pool
  truffles scan -file repos.txt -workers 4
  truffles scan -f ai-100.txt -workers 8 -out secrets.txt
  truffles scan -f ai-100.txt -format csv -workers 8     # ai-100-<ts>.csv
  truffles scan -f ai-100.txt -exclude-paths node_modules,vendor,*.lock

Proxies are scoped to this process only; no HTTP_PROXY env vars are exported,
so your shell and other tools are unaffected.
`

// Main dispatches to a subcommand. It takes arguments rather than reading
// os.Args so it can be called from tests, and it returns errors instead of
// exiting, leaving exit codes to the entry point.
func Main(args []string) error {
	colorMode := "auto"
	if v := os.Getenv("NO_COLOR"); v != "" {
		colorMode = "never"
	}
	for _, a := range args {
		if a == "-color=never" || a == "--color=never" {
			colorMode = "never"
		} else if a == "-color=always" || a == "--color=always" {
			colorMode = "always"
		}
	}
	p := palette{on: resolveColor(colorMode, os.Stdout)}

	if len(args) == 0 {
		p.printBanner(os.Stdout)
		p.printTagline(os.Stdout)
		p.printStyledUsage(os.Stdout)
		fmt.Fprintln(os.Stdout, p.dim("Run `truffles help` for full details."))
		return nil
	}

	// Version early
	for _, a := range args {
		if a == "--version" || a == "-version" || a == "version" || a == "-V" {
			return runVersion()
		}
	}

	// Help is handled before dispatch. Otherwise `truffles -h` falls through to
	// the default "scan" command, where the flag package prints usage to stderr
	// and returns flag.ErrHelp, which would exit 1.
	switch args[0] {
	case "help", "-h", "-help", "--help":
		p.printBanner(os.Stdout)
		p.printTagline(os.Stdout)
		p.printFullHelp(os.Stdout)
		return nil
	}

	cmd, rest := "scan", args
	if args[0][0] != '-' {
		cmd, rest = args[0], args[1:]
	}

	switch cmd {
	case "search":
		return runSearch(rest)
	case "scan":
		return runScan(rest)
	case "wizard":
		return runWizard(rest)
	case "examples", "example":
		p.printExamples(os.Stdout)
		return nil
	case "playbook":
		return runPlaybook(rest)
	case "playbook:gen", "gen:playbook", "pbgen":
		// quick generate
		name := "playbook"
		out := "playbook.yaml"
		for i := 0; i < len(rest); i++ {
			if (rest[i] == "-o" || rest[i] == "--out") && i+1 < len(rest) {
				out = rest[i+1]
				i++
			} else if !strings.HasPrefix(rest[i], "-") {
				name = rest[i]
			}
		}
		pb := genPlaybook(name)
		if err := writePlaybook(pb, out); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", out)
		return nil
	case "playbook:touch", "playbook:example", "playbook:make-example", "pb:example", "pb:touch", "touch-example", "make-example", "example:playbook", "mkplaybook", "playbook:make", "make:playbook", "playbook:touch-example":
		return runPlaybookTouch(rest)
	case "cluster":
		return runCluster(rest)
	case "cluster:coop", "coop":
		return runClusterCoop(rest)
	case "version", "--version", "-version", "-v":
		return runVersion()
	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
	}
	// `truffles search -h` should print usage and succeed, not error out.
	return fs
}

// parseFlags parses flags that may be interspersed with positional args.
// Go's flag package stops at the first non-flag argument, so
// `search torvalds lin* -no-proxy` would silently drop -no-proxy. This
// reorders the args to put flags first, keeping each flag's value with it.
func parseFlags(fs *flag.FlagSet, args []string) error {
	var flagArgs, positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			// Everything after -- is positional even if it looks like a flag.
			// The terminator itself must be carried over, because this function
			// reorders args and would otherwise hand flag.Parse a bare
			// "-not-a-flag" to reject.
			positional = append(positional, "--")
			positional = append(positional, args[i+1:]...)
			break
		}

		// Not a flag (or a bare "-"), so it's positional.
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}

		flagArgs = append(flagArgs, arg)
		if strings.Contains(arg, "=") {
			continue // value is attached
		}

		name := strings.TrimLeft(arg, "-")
		f := fs.Lookup(name)
		if f == nil {
			// Unknown flag; let flag.Parse report it.
			continue
		}
		// Boolean flags take no value.
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		// Consume the following arg as this flag's value.
		if i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}

	return fs.Parse(append(flagArgs, positional...))
}
