// Command truffles finds repositories on GitHub and scans them for leaked
// secrets.
//
// It has two subcommands: `search` discovers repositories, `scan` runs
// trufflehog over a list of them. Run `truffles help` for usage.
package main

import (
	"fmt"
	"os"

	"github.com/adamsiwiec/truffles/internal/cli"
)

func main() {
	if err := cli.Main(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "[x] %v\n", err)
		os.Exit(1)
	}
}
