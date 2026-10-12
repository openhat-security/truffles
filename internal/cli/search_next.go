package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type searchNextOpts struct {
	outPath     string
	incomplete  bool
	failedPages int
	prefixFile  string
	termsFile   string
	skipFile    string
	cleanFile   string
	limit       int
	proxyFile   string
	hasRemotePB bool
}

// nextHint is one “Next:” row: short label (3–5 words) + optional command.
type nextHint struct {
	label string
	cmd   string
	warn  bool // red label, no command (e.g. partial search)
}

func searchNextSteps(o searchNextOpts) []nextHint {
	out := o.outPath
	if out == "" || out == "-" {
		out = "repos.txt"
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = out
	}

	var hints []nextHint

	if o.incomplete {
		hints = append(hints, nextHint{
			label: fmt.Sprintf("%d pages failed", o.failedPages),
			warn:  true,
		})
		hints = append(hints, nextHint{
			label: "Retry search",
			cmd:   buildSearchRerunCmd(o, abs, false),
		})
		hints = append(hints, nextHint{
			label: "More retries/proxies",
			cmd:   buildSearchRerunCmd(o, abs, true),
		})
	}

	if o.hasRemotePB {
		ts := "remote-results-$(date +%Y%m%d-%H%M%S)"
		hints = append(hints, nextHint{
			label: "Scan on cluster",
			cmd: fmt.Sprintf("./truffles cluster run -c remote-playbook.yaml -f %s -data-dir data -out %s -y",
				abs, ts),
		})
		hints = append(hints, nextHint{
			label: "Collect from workers",
			cmd: fmt.Sprintf("./truffles cluster collect -c remote-playbook.yaml -out %s -data-dir data -y",
				ts),
		})
		prefix := o.prefixFile
		if prefix == "" {
			prefix = "examples/prefixes-openai.txt"
		}
		hints = append(hints, nextHint{
			label: "Start continuous loop",
			cmd:   fmt.Sprintf("./truffles job start -c remote-playbook.yaml --prefix-file %s --seed 0", prefix),
		})
	} else {
		hints = append(hints, nextHint{
			label: "Scan repo list",
			cmd:   fmt.Sprintf("./truffles scan -f %s", abs),
		})
		hints = append(hints, nextHint{
			label: "CSV to stdout",
			cmd:   fmt.Sprintf("./truffles scan -f %s -format csv -out -", abs),
		})
	}

	return hints
}

func buildSearchRerunCmd(o searchNextOpts, outAbs string, hardened bool) string {
	var parts []string
	parts = append(parts, "./truffles", "search")
	if o.prefixFile != "" {
		parts = append(parts, "-prefix-file", o.prefixFile)
	}
	if o.termsFile != "" {
		parts = append(parts, "-terms-file", o.termsFile)
	}
	if o.skipFile != "" {
		parts = append(parts, "-skip-file", o.skipFile)
	}
	if o.cleanFile != "" {
		parts = append(parts, "-clean-file", o.cleanFile)
	}
	if o.limit > 0 {
		parts = append(parts, "-limit", fmt.Sprintf("%d", o.limit))
	}
	if hardened {
		parts = append(parts, "-retries", "6", "-no-direct")
		if o.proxyFile != "" {
			parts = append(parts, "-proxy-file", o.proxyFile)
		} else {
			parts = append(parts, "-proxy-file", "data/proxies.txt")
		}
	} else if o.proxyFile != "" {
		parts = append(parts, "-proxy-file", o.proxyFile)
	}
	parts = append(parts, "-seed", "0", "-out", outAbs)
	return strings.Join(parts, " ")
}

func hasRemotePlaybook() bool {
	_, err := os.Stat("remote-playbook.yaml")
	return err == nil
}
