package cli

import (
	"fmt"
	"io"
)

func (p palette) printExamples(w io.Writer) {
	p.printBanner(w)
	p.printTagline(w)
	fmt.Fprintln(w, p.byellow("examples"))
	fmt.Fprintln(w)

	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles search -owner openhat-security -out repos.txt"),
		p.dim("# every public repo under the org"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles search -prefix-file examples/prefixes-openai.txt -limit 500 -seed 0 -out repos.txt"),
		p.dim("# AI key pattern search"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles search 'llm' -limit 100"),
		p.dim("# global search"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles scan -f repos.txt -workers 4"),
		p.dim("# scan from file (default -since 2y)"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles verify -f data/remote-results/results.csv"),
		p.dim("# live-verify secrets without recloning"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles combine data/remote-results-* -o data/all.csv"),
		p.dim("# secret-dedup merge"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles consolidate --update-master --update-clean"),
		p.dim("# roll up data/ into consolidated/"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles job start -c remote-playbook.yaml --prefix-file examples/prefixes-openai.txt"),
		p.dim("# 24/7 search→cluster loop"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles cluster run -c remote-playbook.yaml -f repos.txt -y"),
		p.dim("# SSH workers"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles wizard"),
		p.dim("# interactive walkthrough"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles playbook -f playbook.yaml"),
		p.dim("# author + run a playbook"))
	fmt.Fprintln(w)
}
