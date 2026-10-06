package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runPlaybookTouch(rest []string) error {
	scanner := bufio.NewScanner(os.Stdin)
	out := ""
	for i := 0; i < len(rest); i++ {
		if (rest[i] == "-o" || rest[i] == "--out" || rest[i] == "-f" || rest[i] == "--file") && i+1 < len(rest) {
			out = rest[i+1]
			i++
			continue
		}
		if !strings.HasPrefix(rest[i], "-") {
			out = rest[i]
		}
	}

	if out == "" {
		fmt.Print("Where should the example playbook be written? [./playbook.example.yaml] ")
		if scanner.Scan() {
			ans := strings.TrimSpace(scanner.Text())
			if ans != "" {
				out = ans
			} else {
				out = "./playbook.example.yaml"
			}
		} else {
			out = "./playbook.example.yaml"
		}
	}

	// Expand if relative
	if !filepath.IsAbs(out) {
		cwd, _ := os.Getwd()
		out = filepath.Join(cwd, out)
	}

	dir := filepath.Dir(out)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	pb := genPlaybook("example")
	pb.Description = "example playbook: search and scan"
	pb.Search.Owner = "openhat-security"
	pb.Search.Queries = []string{"*run*"}
	pb.Search.Out = "repos-example.txt"
	pb.Search.Limit = 50
	pb.Search.NoProxy = true
	pb.Scan.File = "repos-example.txt"
	pb.Scan.Format = "pretty"
	pb.Scan.ExcludePaths = []string{"node_modules", "vendor", "*.lock"}

	if err := writePlaybook(pb, out); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", out)
	return nil
}
