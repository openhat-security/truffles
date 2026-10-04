package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func runScan(args []string) error {
	fs := newFlagSet("scan")
	filePath := fs.String("file", "repos.txt", "Path to file with GitHub URLs (one per line)")
	fs.StringVar(filePath, "f", "repos.txt", "Shorthand for -file")
	trufflehogBin := fs.String("bin", "trufflehog", "Path to trufflehog binary")

	workers := fs.Int("workers", 4, "Concurrent repos (helps small repos; big clones stay bandwidth-bound)")
	fs.IntVar(workers, "w", 4, "Shorthand for -workers")
	token := fs.String("token", "", "GitHub token for private repos")
	results := fs.String("results", "verified,unknown", "Result types: verified,unknown,unverified")
	noVerify := fs.Bool("no-verification", false, "Skip live verification (much faster; everything comes back UNVERIFIED)")
	maxDepth := fs.Int("max-depth", 0, "Only scan the last N commits per repo (0 = all history)")
	excludePaths := fs.String("exclude-paths", "", "Comma-separated paths/globs trufflehog should skip")
	jsonOut := fs.Bool("json", true, "Use JSON output")
	out := fs.String("out", "", "Report file (default <input>-<unixtimestamp>.txt; \"-\" for stdout)")
	colorMode := fs.String("color", "auto", "Colourise report: auto, always, never")
	format := fs.String("format", "pretty", "Report format: pretty, csv, jsonl")
	progressEvery := fs.Duration("progress", 10*time.Second, "Log progress this often; 0 disables the heartbeat")
	verbose := fs.Bool("v", false, "Also show trufflehog's own log output")
	if err := parseFlags(fs, args); err != nil {

		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	switch *format {
	case "pretty", "csv", "jsonl":
	default:
		return fmt.Errorf("unknown -format %q (want pretty, csv or jsonl)", *format)
	}
	if *format != "pretty" && !*jsonOut {
		return fmt.Errorf("-format %s needs -json (trufflehog text output cannot be parsed)", *format)
	}

	if _, err := exec.LookPath(*trufflehogBin); err != nil {
		return fmt.Errorf("trufflehog binary %q not usable (override with -bin): %w", *trufflehogBin, err)
	}

	urls, err := readURLs(*filePath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", *filePath, err)
	}
	if len(urls) == 0 {
		fmt.Fprintf(os.Stderr, "[!] no URLs found in %s\n", *filePath)
		return nil
	}
	if *workers < 1 {
		*workers = 1
	}

	sp := palette{resolveColor(*colorMode, os.Stderr)}
	statusf := func(format string, a ...any) {
		fmt.Fprintf(os.Stderr, format+"\n", a...)
	}

	reportPath := *out
	toStdout := reportPath == "-"
	if reportPath == "" {
		reportPath = defaultReportName(*filePath, *format)
	}

	var dest *os.File
	if toStdout {
		dest = os.Stdout
	} else {
		f, err := os.Create(reportPath)
		if err != nil {
			return err
		}
		defer f.Close()
		dest = f
	}
	rp := palette{resolveColor(*colorMode, dest)}

	bw := bufio.NewWriterSize(dest, 32*1024)

	var repMu sync.Mutex

	if toStdout {
		statusf("%s", sp.dim("[*] report -> stdout (pass -out <file> to write a file)"))
	} else {
		statusf("%s", sp.dim("[*] report -> "+reportPath))
	}
	statusf("%s", sp.dim(fmt.Sprintf("[*] %d repos, %d workers", len(urls), *workers)))
	if *verbose {
		statusf("%s", sp.dim("[*] -v: trufflehog's own log output will be shown"))
	}

	var (
		wg         sync.WaitGroup
		failed     int64
		done       int64
		findings   int64
		verified   int64
		inFlight   int64
		byDetector sync.Map
		started    = time.Now()
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Heartbeat. A 100-repo scan at 4 workers is ~25 sequential clones; with
	// no output that is indistinguishable from a hang.
	var hb sync.WaitGroup
	if *progressEvery > 0 {
		hb.Add(1)
		go func() {
			defer hb.Done()
			t := time.NewTicker(*progressEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					d := atomic.LoadInt64(&done)
					f := atomic.LoadInt64(&failed)
					el := time.Since(started)
					line := fmt.Sprintf("[*] %d/%d done, %d failed, %d in flight, %d finding(s) (%d verified), %s elapsed",
						d, len(urls), f, atomic.LoadInt64(&inFlight),
						atomic.LoadInt64(&findings), atomic.LoadInt64(&verified), el.Round(time.Second))
					if d > 0 {
						per := el.Seconds() / float64(d)
						rem := (float64(len(urls)) - float64(d)) * per
						line += fmt.Sprintf(", ~%s left",
							time.Duration(rem*float64(time.Second)).Round(time.Second))
					}
					statusf("%s", sp.dim(line))
				}
			}
		}()
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	var interrupted atomic.Bool
	go func() {
		<-sigc
		interrupted.Store(true)
		repMu.Lock()

		if *format == "pretty" {
			fmt.Fprintf(bw, "\n%s\n", rp.bold("=== Interrupted ==="))
			fmt.Fprintf(bw, "  %d/%d repos completed, %d finding(s), %s elapsed\n",
				atomic.LoadInt64(&done), len(urls), atomic.LoadInt64(&findings),
				time.Since(started).Round(time.Second))
			fmt.Fprintf(bw, "  stopped early (SIGINT/SIGTERM); everything above is final\n")
		}
		bw.Flush()
		dest.Sync()
		repMu.Unlock()
		if !toStdout {
			statusf("%s", sp.bold("[!] interrupted — partial report saved to "+reportPath))
		}
		cancel()
		os.Exit(130)
	}()

	if *format == "csv" {
		repMu.Lock()

		writeCSVHeader(bw)
		bw.Flush()
		dest.Sync()
		repMu.Unlock()
	}

	ch := make(chan string, len(urls))
	for _, u := range urls {
		ch <- u
	}
	close(ch)

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range ch {
				atomic.AddInt64(&inFlight, 1)
				res := scanRepo(ctx, *trufflehogBin, repo, scanOptions{
					token: *token, results: *results, jsonOut: *jsonOut,
					noVerify: *noVerify, maxDepth: *maxDepth, excludePaths: *excludePaths,
				})
				atomic.AddInt64(&inFlight, -1)
				atomic.AddInt64(&done, 1)
				atomic.AddInt64(&findings, int64(len(res.Findings)))
				if res.Err != nil {
					atomic.AddInt64(&failed, 1)
				}
				for _, fd := range res.Findings {
					if fd.Verified {
						atomic.AddInt64(&verified, 1)
					}
					name := fd.DetectorName
					if name == "" {
						name = "unknown"
					}
					v, _ := byDetector.LoadOrStore(name, new(int64))
					atomic.AddInt64(v.(*int64), 1)
				}

				name := shortRepo(repo)
				switch {
				case res.Err != nil:
					statusf("%s %s %s", sp.red("[!!]"), pad(name, 45),
						sp.red(fmt.Sprintf("%8s  %v", res.Duration.Round(time.Millisecond), res.Err)))
				case len(res.Findings) > 0:
					statusf("%s %s %s", sp.byellow("[++]"), pad(name, 45),
						sp.byellow(fmt.Sprintf("%8s  %d finding(s)", res.Duration.Round(time.Millisecond), len(res.Findings))))
				case res.Lines > 0:
					statusf("%s %s %s", sp.cyan("[ok]"), pad(name, 45),
						sp.cyan(fmt.Sprintf("%8s  %d output line(s)", res.Duration.Round(time.Millisecond), res.Lines)))
				default:
					statusf("%s %s %s", sp.green("[ok]"), pad(name, 45),
						sp.green(fmt.Sprintf("%8s  clean", res.Duration.Round(time.Millisecond))))
				}
				if *verbose && strings.TrimSpace(res.Logs) != "" {
					for _, l := range strings.Split(strings.TrimRight(res.Logs, "\n"), "\n") {
						statusf("     %s", sp.dim(l))
					}
				}

				repMu.Lock()
				switch *format {
				case "csv":
					writeCSV(bw, res)
				case "jsonl":
					writeJSONL(bw, res)
				default:
					renderRepo(bw, rp, repo, res)
				}

				bw.Flush()
				dest.Sync()
				repMu.Unlock()

				if len(res.Findings) > 0 && !toStdout && *format == "pretty" {
					var sb strings.Builder
					renderRepo(prettyWriter{&sb}, sp, repo, res)
					statusf("%s", strings.TrimRight(sb.String(), "\n"))
				}
			}
		}()
	}
	wg.Wait()
	cancel()
	hb.Wait()

	elapsed := time.Since(started).Round(time.Second)
	fail := atomic.LoadInt64(&failed)

	summaryToStderr := *format != "pretty"
	repMu.Lock()
	if summaryToStderr {
		repMu.Unlock()
		statusf("%s", sp.bold("=== Summary ==="))
		statusf("  repos scanned %d, failed %d, findings %d (%d verified), elapsed %s",
			len(urls), fail, atomic.LoadInt64(&findings), atomic.LoadInt64(&verified), elapsed)
		var rows []struct {
			name string
			n    int64
		}
		byDetector.Range(func(k, v any) bool {
			rows = append(rows, struct {
				name string
				n    int64
			}{k.(string), atomic.LoadInt64(v.(*int64))})
			return true
		})
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].n != rows[j].n {
				return rows[i].n > rows[j].n
			}
			return rows[i].name < rows[j].name
		})
		for _, r := range rows {
			statusf("    %s %d", pad(r.name, 30), r.n)
		}
		if !toStdout {
			statusf("%s", sp.bold("[+] report -> "+reportPath))
		}
		return nil
	}
	fmt.Fprintf(bw, "\n%s\n", rp.bold("=== Summary ==="))
	fmt.Fprintf(bw, "  %s %d\n", rp.dim(fmt.Sprintf("%-22s", "Repos scanned")), len(urls))
	fmt.Fprintf(bw, "  %s %d\n", rp.dim(fmt.Sprintf("%-22s", "Failed")), fail)
	fmt.Fprintf(bw, "  %s %d\n", rp.dim(fmt.Sprintf("%-22s", "Findings")), atomic.LoadInt64(&findings))
	v := atomic.LoadInt64(&verified)
	verdict := rp.yellow(fmt.Sprintf("%d unverified", atomic.LoadInt64(&findings)-v))
	if v > 0 {
		verdict = rp.bgreen(fmt.Sprintf("%d verified", v)) + ", " + verdict
	}
	fmt.Fprintf(bw, "  %s %s\n", rp.dim(fmt.Sprintf("%-22s", "Verification")), verdict)
	fmt.Fprintf(bw, "  %s %s\n", rp.dim(fmt.Sprintf("%-22s", "Elapsed")), elapsed)

	var rows []struct {
		name string
		n    int64
	}
	byDetector.Range(func(k, v any) bool {
		rows = append(rows, struct {
			name string
			n    int64
		}{k.(string), atomic.LoadInt64(v.(*int64))})
		return true
	})
	if len(rows) > 0 {
		fmt.Fprintf(bw, "\n  %s\n", rp.bold("By detector"))
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].n != rows[j].n {
				return rows[i].n > rows[j].n
			}
			return rows[i].name < rows[j].name
		})
		for _, r := range rows {
			fmt.Fprintf(bw, "    %s %s\n", pad(r.name, 30), rp.cyan(fmt.Sprintf("%d", r.n)))
		}
	}
	bw.Flush()
	dest.Sync()
	repMu.Unlock()

	statusf("%s", sp.dim(fmt.Sprintf("[+] done — %d repos, %d finding(s), %s", len(urls), atomic.LoadInt64(&findings), elapsed)))
	if !toStdout {
		statusf("%s", sp.bold("[+] report -> "+reportPath))
	}
	return nil
}

// scanOptions carries the knobs that only affect how fast a single repo is
// scanned, as opposed to what ends up in the report.
type scanOptions struct {
	token        string
	results      string
	jsonOut      bool
	noVerify     bool
	maxDepth     int
	excludePaths string
}

// scanRepo runs trufflehog over one repo, capturing findings on stdout and
// trufflehog's own logging on stderr separately.
func scanRepo(ctx context.Context, bin, repo string, o scanOptions) Result {
	args := []string{"git", repo}
	if o.jsonOut {
		args = append(args, "--json")
	}
	args = append(args, "--results="+o.results)
	if o.token != "" {
		args = append(args, "--token="+o.token)
	}
	if o.noVerify {

		args = append(args, "--no-verification")
	}
	if o.maxDepth > 0 {
		args = append(args, fmt.Sprintf("--max-depth=%d", o.maxDepth))
	}
	if o.excludePaths != "" {
		args = append(args, "--exclude-paths="+o.excludePaths)
	}

	res := Result{Repo: repo}

	start := time.Now()
	var stdout, stderr []byte
	var err error
	for attempt := 0; ; attempt++ {
		cmd := exec.CommandContext(ctx, bin, args...)
		var errBuf strings.Builder
		cmd.Stderr = &errBuf
		stdout, err = cmd.Output()
		stderr = []byte(errBuf.String())
		if err == nil {
			break
		}

		if errors.Is(err, exec.ErrNotFound) {
			res.Duration = time.Since(start)
			res.Err = fmt.Errorf("trufflehog binary %q not found in PATH", bin)
			return res
		}
		if attempt == scanAttempts-1 || permanentScanError(stderr) {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	if err != nil {

		res.Err = scanError(err, stderr)
	}

	res.Duration = time.Since(start)
	res.Output = string(stdout)
	res.Logs = string(stderr)
	res.Findings, res.Lines = parseFindings(res.Output, o.jsonOut)
	return res
}

func readURLs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var urls []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, scanner.Err()
}

// scanAttempts bounds per-repo retries, so one bad repo cannot stall the run.
const scanAttempts = 3

// permanentScanError reports whether trufflehog's stderr says the failure will
// not go away on its own. Retrying these just burns time and, for private
// repos, hammers GitHub.
func permanentScanError(stderr []byte) bool {
	s := string(stderr)
	for _, m := range []string{
		"Repository not found",
		"Authentication failed",
		"could not read Username",
		"403 Forbidden",
		"does not exist",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// scanError turns a non-zero exit into a short readable reason. trufflehog logs
// JSON to stderr, so the useful message is pulled out of the last line that
// carries an "error" field rather than dumping kilobytes of log noise.
func scanError(err error, stderr []byte) error {
	reason := ""
	for _, line := range strings.Split(string(stderr), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var rec struct {
			Error string `json:"error"`
			Msg   string `json:"msg"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Error != "" {
			reason = rec.Error
		} else if rec.Msg != "" && reason == "" {
			reason = rec.Msg
		}
	}
	if reason == "" {
		if e := new(exec.ExitError); errors.As(err, &e) {
			return fmt.Errorf("trufflehog exited %d", e.ExitCode())
		}
		return err
	}
	reason = strings.Join(strings.Fields(reason), " ")
	if len(reason) > 160 {
		reason = reason[:157] + "..."
	}
	return errors.New(reason)
}
