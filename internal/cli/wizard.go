package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func runWizard(rest []string) error {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print(bold("truffles wizard") + "\n\n")
	fmt.Print("I'll walk you through finding and scanning repos for secrets.\n\n")

	// Mode
	fmt.Print("1) Search by owner (enumerate all repos for a user/org, filter by patterns)\n")
	fmt.Print("2) Global search (GitHub search API)\n")
	fmt.Print("Choose [1/2]: ")
	if !scanner.Scan() {
		return nil
	}
	mode := strings.TrimSpace(scanner.Text())

	var cmd []string
	switch mode {
	case "1", "owner", "enumerate":
		fmt.Print("Owner(s) (comma-separated, e.g. BurntSushi,torvalds): ")
		if !scanner.Scan() {
			return nil
		}
		owner := strings.TrimSpace(scanner.Text())
		if owner == "" {
			return fmt.Errorf("owner required")
		}
		cmd = append(cmd, "search", "-owner", owner)

		fmt.Print("Patterns (space-separated, or leave empty for all): ")
		if !scanner.Scan() {
			pat := strings.TrimSpace(scanner.Text())
			if pat != "" {
				cmd = append(cmd, strings.Fields(pat)...)
			}
		} else {
			pat := strings.TrimSpace(scanner.Text())
			if pat != "" {
				cmd = append(cmd, strings.Fields(pat)...)
			}
		}
	case "2", "global", "search":
		fmt.Print("Search query (e.g. 'llm' or 'language:python rag'): ")
		if !scanner.Scan() {
			return nil
		}
		q := strings.TrimSpace(scanner.Text())
		if q == "" {
			return fmt.Errorf("query required")
		}
		cmd = append(cmd, "search", q)
		fmt.Print("Max results (default 100, 0=unlimited): ")
		if scanner.Scan() {
			if v := strings.TrimSpace(scanner.Text()); v != "" {
				cmd = append(cmd, "-limit", v)
			}
		}
	default:
		return fmt.Errorf("invalid choice")
	}

	fmt.Print("Save repo list to file? [repos.txt]: ")
	var outFile string
	if scanner.Scan() {
		outFile = strings.TrimSpace(scanner.Text())
	}
	if outFile == "" {
		outFile = "repos.txt"
	}
	cmd = append(cmd, "-out", outFile)

	fmt.Printf("\nStep 1: %s %s\n", bold("truffles"), strings.Join(cmd, " "))
	fmt.Print("Run this search now? [Y/n]: ")
	if scanner.Scan() {
		ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if ans != "n" && ans != "no" {
			if err := runSearch(cmd[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "search failed: %v\n", err)
			}
		}
	}

	fmt.Printf("\nStep 2: scan repos from %s\n", outFile)
	scanCmd := []string{"scan", "-f", outFile}
	fmt.Print("Workers (default 4): ")
	if scanner.Scan() {
		if v := strings.TrimSpace(scanner.Text()); v != "" {
			scanCmd = append(scanCmd, "-workers", v)
		}
	}
	fmt.Print("Format (pretty/csv/jsonl) [csv]: ")
	if scanner.Scan() {
		if v := strings.TrimSpace(scanner.Text()); v != "" && v != "csv" {
			scanCmd = append(scanCmd, "-format", v)
		}
	}
	fmt.Printf("\n%s %s\n", bold("truffles"), strings.Join(scanCmd, " "))
	fmt.Print("Run scan now? [y/N]: ")
	if scanner.Scan() {
		ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if ans == "y" || ans == "yes" {
			if err := runScan(scanCmd[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "scan failed: %v\n", err)
			}
		}
	}

	fmt.Println("\nDone. Use `truffles help` for more options.")
	return nil
}

func bold(s string) string { return "\x1b[1m" + s + "\x1b[0m" }
