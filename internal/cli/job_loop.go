package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// jobLoopOpts mirrors scripts/run-loop.sh flags.
type jobLoopOpts struct {
	Playbook   string
	PrefixFile string
	TermsFile  string
	SearchSeed int64
	SkipFile   string
	CleanFile  string
	DataDir    string
	Limit      int
	SleepSecs  int
	Workers    int
	Mode       string // split | playbook
	Once       bool
	Bin        string // unused for in-process; kept for meta/cmd display
}

func jobRun(args []string) error {
	opts, err := parseJobLoopFlags(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stderr, jobRunUsage())
			return nil
		}
		return err
	}
	return runJobLoop(opts)
}

func jobRunUsage() string {
	return `truffles job run — foreground continuous loop (used by job start)

usage:
  truffles job run -c <playbook.yaml> [flags]

Same flags as job start. Prefer: truffles job start -c …
`
}

func parseJobLoopFlags(args []string) (*jobLoopOpts, error) {
	fs := flag.NewFlagSet("job run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	playbook := fs.String("c", "", "playbook YAML")
	fs.StringVar(playbook, "playbook-file", "", "playbook YAML")
	prefixFile := fs.String("prefix-file", "examples/prefixes.txt", "prefix file for search mode")
	termsFile := fs.String("terms-file", "", "search terms file")
	searchSeed := fs.Int64("seed", 0, "search shuffle seed (0 = random each cycle)")
	limit := fs.Int("limit", 500, "repos per cycle")
	sleepSecs := fs.Int("sleep", 60, "seconds between cycles")
	fullPlaybook := fs.Bool("full-playbook", false, "use truffles playbook each cycle")
	once := fs.Bool("once", false, "single cycle then exit")
	skipFile := fs.String("skip-file", "", "override skip file")
	cleanFile := fs.String("clean-file", "", "override clean file")
	workers := fs.Int("workers", 8, "search workers")
	dataDir := fs.String("data-dir", "data", "data directory")
	bin := fs.String("bin", "", "ignored (loop is in-process)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *playbook == "" && fs.NArg() > 0 {
		*playbook = fs.Arg(0)
	}
	if strings.TrimSpace(*playbook) == "" {
		return nil, fmt.Errorf("playbook required: truffles job run -c remote-playbook.yaml")
	}
	if _, err := os.Stat(*playbook); err != nil {
		return nil, fmt.Errorf("playbook %s: %w", *playbook, err)
	}
	skip := *skipFile
	if skip == "" {
		skip = filepath.Join(*dataDir, "scanned-repos.txt")
	}
	clean := *cleanFile
	if clean == "" {
		clean = filepath.Join(*dataDir, "clean-repos.txt")
	}
	mode := "split"
	if *fullPlaybook {
		mode = "playbook"
	}
	return &jobLoopOpts{
		Playbook:   *playbook,
		PrefixFile: *prefixFile,
		TermsFile:  *termsFile,
		SearchSeed: *searchSeed,
		SkipFile:   skip,
		CleanFile:  clean,
		DataDir:    *dataDir,
		Limit:      *limit,
		SleepSecs:  *sleepSecs,
		Workers:    *workers,
		Mode:       mode,
		Once:       *once,
		Bin:        *bin,
	}, nil
}

func githubTokensFromEnv() string {
	tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if t2 := strings.TrimSpace(os.Getenv("GITHUB_TOKEN_2")); t2 != "" {
		if tok != "" {
			tok = tok + "," + t2
		} else {
			tok = t2
		}
	}
	return tok
}

func runJobLoop(opts *jobLoopOpts) error {
	if _, err := os.Stat("remote.env"); err == nil {
		_ = loadEnvFile("remote.env")
	}

	var stop atomic.Bool
	var currentOut, currentRepos atomic.Value
	currentOut.Store("")
	currentRepos.Store("")

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigc
		stop.Store(true)
		fmt.Fprintln(os.Stderr, "[!] stop requested — finishing current step…")
		if dir, _ := currentOut.Load().(string); dir != "" {
			printInterruptedLocalPaths(os.Stderr, dir)
			if repos, _ := currentRepos.Load().(string); repos != "" {
				fmt.Fprintf(os.Stderr, "  %s\n", absPathOrSelf(repos))
			}
		}
	}()

	jobLog := func(format string, a ...any) {
		prefix := time.Now().Format("2006-01-02 15:04:05")
		msg := fmt.Sprintf(format, a...)
		if id := os.Getenv("JOB_ID"); id != "" {
			fmt.Fprintf(os.Stderr, "[%s] [%s] %s\n", prefix, id, msg)
		} else {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", prefix, msg)
		}
	}

	if id := os.Getenv("JOB_ID"); id != "" {
		jobLog("job_id=%s", id)
	}
	jobLog("loop start playbook=%s mode=%s prefix=%s limit=%d sleep=%ds",
		opts.Playbook, opts.Mode, opts.PrefixFile, opts.Limit, opts.SleepSecs)

	cycle := 0
	for !stop.Load() {
		cycle++
		ts := time.Now().Format("20060102-150405")
		outName := "remote-results-" + ts
		repos := filepath.Join(opts.DataDir, "repos-loop-"+ts+".txt")
		_ = os.MkdirAll(opts.DataDir, 0o755)
		outDir := filepath.Join(opts.DataDir, outName)
		_ = os.MkdirAll(outDir, 0o755)
		currentOut.Store(outDir)
		currentRepos.Store(repos)

		updateClean := false
		if opts.Mode == "playbook" {
			jobLog("cycle %d — playbook → %s", cycle, outDir)
			args := []string{"-f", opts.Playbook, "-y", "-data-dir", opts.DataDir, "-out", outName}
			if err := runPlaybook(args); err != nil {
				jobLog("playbook failed: %v — continuing", err)
			}
			updateClean = true
		} else {
			tok := githubTokensFromEnv()
			if tok == "" {
				jobLog("GITHUB_TOKEN not set (remote.env) — cannot search")
				if opts.Once || stop.Load() {
					break
				}
				sleepJob(opts.SleepSecs, &stop)
				continue
			}
			jobLog("cycle %d — search (prefix=%s, limit=%d) → %s", cycle, opts.PrefixFile, opts.Limit, repos)
			searchArgs := []string{
				"-out", repos,
				"-skip-file", opts.SkipFile,
				"-clean-file", opts.CleanFile,
				"-limit", strconv.Itoa(opts.Limit),
				"-workers", strconv.Itoa(opts.Workers),
				"-token", tok,
				"-seed", strconv.FormatInt(opts.SearchSeed, 10),
			}
			if opts.PrefixFile != "" {
				searchArgs = append([]string{"-prefix-file", opts.PrefixFile}, searchArgs...)
			}
			if opts.TermsFile != "" {
				searchArgs = append([]string{"-terms-file", opts.TermsFile}, searchArgs...)
			}
			if err := runSearch(searchArgs); err != nil {
				jobLog("search failed: %v — continuing", err)
			} else {
				st, err := os.Stat(repos)
				if err != nil || st.Size() == 0 {
					jobLog("no new repos — sleeping %ds", opts.SleepSecs)
				} else {
					n := countURLLines(repos)
					jobLog("cycle %d — cluster run %d repos → %s", cycle, n, outDir)
					clusterArgs := []string{
						"run", "-c", opts.Playbook,
						"-f", repos,
						"-out", outName,
						"-data-dir", opts.DataDir,
						"-y",
					}
					if err := runCluster(clusterArgs); err != nil {
						jobLog("cluster run failed: %v — continuing", err)
					}
					updateClean = true
				}
			}
		}

		if updateClean {
			jobLog("updating %s", opts.CleanFile)
			if _, _, err := rebuildCleanRepos(cleanRebuildOpts{
				Out:            opts.CleanFile,
				IncludeScanned: true,
				DataDir:        opts.DataDir,
			}); err != nil {
				jobLog("update-clean-repos failed: %v", err)
			}
			verd := filepath.Join(outDir, combinedVerifiedCSVName)
			if st, err := os.Stat(verd); err == nil && !st.IsDir() {
				if lines, err := countFileLines(verd); err == nil {
					jobLog("verified spreadsheet: %s (%d lines incl. header)", verd, lines)
				}
			}
		}

		if opts.Once || stop.Load() {
			break
		}
		jobLog("sleep %ds before next cycle", opts.SleepSecs)
		sleepJob(opts.SleepSecs, &stop)
	}
	jobLog("loop stopped after %d cycle(s)", cycle)
	return nil
}

func sleepJob(secs int, stop *atomic.Bool) {
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for time.Now().Before(deadline) && !stop.Load() {
		time.Sleep(200 * time.Millisecond)
	}
}

// selfExecutable returns the path to the current binary.
func selfExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// readURLsList loads a URL list, ignoring errors (empty on failure).
func readURLsList(path string) []string {
	urls, err := readURLs(path)
	if err != nil {
		return nil
	}
	return urls
}
