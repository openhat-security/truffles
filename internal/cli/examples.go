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
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles search -owner openhat-security '*run*' -out repos.txt"),
		p.dim("# glob filter on repo names"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles search -owner openhat-security -regex '^run' -out repos.txt"),
		p.dim("# regex filter"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles search 'llm' -limit 100"),
		p.dim("# global search"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles search -no-proxy -owner openhat-security"),
		p.dim("# skip proxy pool"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles scan -f repos.txt -workers 4"),
		p.dim("# scan from file"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles scan -f repos.txt -format csv"),
		p.dim("# CSV output with timestamped filename"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles scan -f repos.txt -exclude-paths node_modules,vendor,*.lock"),
		p.dim("# skip noisy paths"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("truffles wizard"),
		p.dim("# interactive walkthrough"))
	fmt.Fprintf(w, "  %s %s\n", p.cyan("vim playbook.yaml && truffles playbook -f playbook.yaml"),
		p.dim("# author + run a playbook"))
	fmt.Fprintln(w)
}
