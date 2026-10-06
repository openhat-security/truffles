package cli

import (
	"fmt"
	"os"

	"github.com/adamsiwiec/truffles/internal/version"
)

func runVersion() error {
	fmt.Fprintln(os.Stdout, version.Version)
	return nil
}
