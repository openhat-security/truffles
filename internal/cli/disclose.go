package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adamsiwiec/truffles/internal/disclose"
)

func runDisclose(rest []string) error {
	fs := flag.NewFlagSet("disclose", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `truffles disclose - coordinated disclosure from scan CSV reports

Reads truffles results CSV (detector,verified,…,repository_url,…), groups by
GitHub repo, then:
  • private vulnerability reporting ON  → private advisory report
  • PVR OFF                             → public issue pointing to -contact
                                          (no secrets/paths in the issue)

Full detail is always written under -out as local drafts.

usage:
  truffles disclose -f data/remote-results/results.csv
  truffles disclose -f data/remote-results -submit

  -submit uses only -token or OPENHAT_BOT_GH_TOKEN (OpenHat bot account).
  Dry-run may fall back to GITHUB_TOKEN / saved token for PVR checks.
  Load remote.env: truffles disclose -env-file remote.env …

flags:
`)
		fs.PrintDefaults()
	}

	f := fs.String("f", "", "results CSV file, or directory containing results*.csv")
	token := fs.String("token", "", "GitHub token (saved under ~/.config/truffles/ if passed)")
	submit := fs.Bool("submit", false, "actually POST advisory/issue (default: dry-run)")
	includeSecret := fs.Bool("include-secret", false, "include plaintext secrets in private advisory body only")
	contact := fs.String("contact", disclose.DefaultContact, "email in public issue CTA")
	all := fs.Bool("all", false, "include unverified findings (default: verified only)")
	out := fs.String("out", "", "drafts/log directory (default: <csv-dir>/disclose)")
	maxN := fs.Int("max", 50, "max repositories to process")
	noIssue := fs.Bool("no-issue", false, "if PVR off, write local draft only (do not open an issue)")
	force := fs.Bool("force", false, "submit even if disclose-state matches (ignore dedupe)")
	envFile := fs.String("env-file", "", "dotenv for OPENHAT_BOT_GH_TOKEN (does not override existing env)")

	if err := fs.Parse(rest); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *f == "" {
		fs.Usage()
		return fmt.Errorf("-f <results.csv|dir> required")
	}

	if strings.TrimSpace(*envFile) != "" {
		if err := loadEnvFile(*envFile); err != nil {
			return fmt.Errorf("env-file: %w", err)
		}
	}

	tok, tokSrc, err := resolveDiscloseGitHubToken(*token, *submit)
	if err != nil {
		return err
	}
	if *submit && tok == "" {
		return fmt.Errorf("-submit needs OpenHat bot token (-token or OPENHAT_BOT_GH_TOKEN)")
	}

	findings, err := disclose.ParseCSVFiles(*f)
	if err != nil {
		return err
	}
	reports := disclose.GroupByRepo(findings, !*all)
	if len(reports) == 0 {
		return fmt.Errorf("no github.com findings to disclose (rows=%d, verifiedOnly=%v)", len(findings), !*all)
	}
	if *maxN > 0 && len(reports) > *maxN {
		fmt.Fprintf(os.Stderr, "[*] capping at %d repos (%d matched)\n", *maxN, len(reports))
		reports = reports[:*maxN]
	}

	outDir := *out
	if outDir == "" {
		base := *f
		if st, err := os.Stat(*f); err == nil && !st.IsDir() {
			base = filepath.Dir(*f)
		}
		outDir = filepath.Join(base, "disclose")
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	statePath := filepath.Join(outDir, "disclose-state.json")
	state, err := loadDiscloseState(statePath)
	if err != nil {
		return err
	}

	client := disclose.NewClient(tok)
	mode := "dry-run"
	if *submit {
		mode = "submit"
	}
	fmt.Printf("[*] disclose %s — %d repo(s), drafts → %s\n", mode, len(reports), outDir)
	if tok == "" {
		fmt.Println("[*] no token — PVR check skipped; dry-run assumes issue fallback when PVR unknown")
	} else if login, err := client.ViewerLogin(); err != nil {
		fmt.Fprintf(os.Stderr, "[!] could not verify GitHub identity: %v\n", err)
	} else {
		fmt.Printf("[*] GitHub identity: @%s (token from %s)\n", login, tokSrc)
	}

	var (
		nPVR, nIssue, nDraft, nSkip int
	)
	for _, rr := range reports {
		hash := reportHash(rr, *includeSecret)
		if prev, ok := state.Submitted[rr.Key()]; ok && prev.Hash == hash {
			skip, skipNote := discloseSkipReason(*force, tok != "", prev, client)
			if skip {
				fmt.Printf("[*] skip %s (%s)\n", rr.Key(), skipNote)
				nSkip++
				continue
			}
			if skipNote != "" {
				fmt.Printf("[*] %s — %s\n", rr.Key(), skipNote)
			}
		}

		pvrOn := false
		if tok != "" {
			enabled, err := client.CheckPVR(rr.Owner, rr.Repo)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[!] PVR check %s: %v — treating as off\n", rr.Key(), err)
			} else {
				pvrOn = enabled
			}
		}

		action := "draft-only"
		url := ""
		switch {
		case pvrOn:
			action = "pvr"
			draft := disclose.LocalDraft(rr, action, *contact, *includeSecret)
			_ = os.WriteFile(filepath.Join(outDir, safeName(rr.Owner)+"__"+safeName(rr.Repo)+".md"), []byte(draft), 0644)
			if *submit {
				u, err := client.SubmitPrivateReport(rr, *includeSecret)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[x] private report %s: %v\n", rr.Key(), err)
					nDraft++
					continue
				}
				url = u
				fmt.Printf("[+] PVR %s → %s\n", rr.Key(), url)
				nPVR++
				state.Submitted[rr.Key()] = discloseRecord{Mode: action, Hash: hash, URL: url, At: time.Now().UTC()}
			} else {
				fmt.Printf("[dry-run] would POST private report for %s (%d finding(s))\n", rr.Key(), len(rr.Findings))
				nPVR++
			}
		case *noIssue:
			action = "draft-only"
			draft := disclose.LocalDraft(rr, action, *contact, *includeSecret)
			_ = os.WriteFile(filepath.Join(outDir, safeName(rr.Owner)+"__"+safeName(rr.Repo)+".md"), []byte(draft), 0644)
			fmt.Printf("[*] draft-only %s (PVR off, -no-issue)\n", rr.Key())
			nDraft++
		default:
			action = "issue"
			draft := disclose.LocalDraft(rr, action, *contact, *includeSecret)
			_ = os.WriteFile(filepath.Join(outDir, safeName(rr.Owner)+"__"+safeName(rr.Repo)+".md"), []byte(draft), 0644)
			if *submit {
				u, err := client.CreateIssue(rr.Owner, rr.Repo, *contact)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[x] issue %s: %v — draft kept\n", rr.Key(), err)
					nDraft++
					continue
				}
				url = u
				fmt.Printf("[+] issue %s → %s\n", rr.Key(), url)
				nIssue++
				state.Submitted[rr.Key()] = discloseRecord{Mode: action, Hash: hash, URL: url, At: time.Now().UTC()}
			} else {
				fmt.Printf("[dry-run] would open issue on %s (contact %s)\n", rr.Key(), *contact)
				nIssue++
			}
		}
	}

	if err := saveDiscloseState(statePath, state); err != nil {
		return err
	}
	fmt.Printf("[+] done — pvr=%d issue=%d draft=%d skipped=%d\n", nPVR, nIssue, nDraft, nSkip)
	if !*submit {
		fmt.Println("re-run with -submit to file reports/issues (token remembered after first use)")
	}
	return nil
}

type discloseRecord struct {
	Mode string    `json:"mode"`
	Hash string    `json:"hash"`
	URL  string    `json:"url,omitempty"`
	At   time.Time `json:"at"`
}

type discloseState struct {
	Submitted map[string]discloseRecord `json:"submitted"`
}

func loadDiscloseState(path string) (*discloseState, error) {
	st := &discloseState{Submitted: map[string]discloseRecord{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, err
	}
	if st.Submitted == nil {
		st.Submitted = map[string]discloseRecord{}
	}
	return st, nil
}

func saveDiscloseState(path string, st *discloseState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func reportHash(r disclose.RepoReport, includeSecret bool) string {
	h := sha256.Sum256([]byte(disclose.PrivateDescription(r, includeSecret)))
	return hex.EncodeToString(h[:8])
}

// discloseSkipReason decides whether to skip a repo already in disclose-state with the same hash.
func discloseSkipReason(force, hasToken bool, prev discloseRecord, client *disclose.Client) (skip bool, note string) {
	if force {
		return false, ""
	}
	note = "already submitted as " + prev.Mode
	if !hasToken || client == nil || prev.Mode != "issue" || prev.URL == "" {
		return true, note
	}
	owner, repo, num, ok := disclose.ParseIssueURL(prev.URL)
	if !ok {
		return true, note
	}
	open, err := client.IssueOpen(owner, repo, num)
	if err != nil {
		return true, note + " (could not re-check issue: " + err.Error() + ")"
	}
	if open {
		return true, note
	}
	return false, "re-disclosing (previous issue closed or removed)"
}
