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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/adamsiwiec/truffles/internal/proxy"
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
	excludePaths := fs.String("exclude-paths", "", "Comma-separated globs to skip (trufflehog --exclude-globs)")
	jsonOut := fs.Bool("json", true, "Use JSON output")
	out := fs.String("out", "", "Report file (default <input>-<unixtimestamp>.txt; \"-\" for stdout)")
	colorMode := fs.String("color", "auto", "Colourise report: auto, always, never")
	format := fs.String("format", "csv", "Report format: pretty, csv, jsonl (pretty+csv both written to disk)")
	skipFile := fs.String("skip-file", "", "Repos already scanned (one URL per line); matching repos are skipped")
	appendScanned := fs.String("append-scanned", "", "Append successfully scanned URLs here (master list; often same as -skip-file)")
	progressEvery := fs.Duration("progress", 10*time.Second, "Log progress this often; 0 disables the heartbeat")
	verbose := fs.Bool("v", false, "Also show trufflehog's own log output")
	// Default true: free proxies are a poor fit for multi-GB git packs and
	// stall parallel workers. Opt into the pool with -no-proxy=false.
	noProxy := fs.Bool("no-proxy", true, "Clone directly (default); set -no-proxy=false to use the proxy pool")
	poolSize := fs.Int("pool-size", proxy.DefaultPoolSize, "Target number of validated proxies")
	probePar := fs.Int("probe-par", proxy.DefaultProbePar, "Concurrent proxy validation probes")
	poolWait := fs.Duration("pool-wait", 60*time.Second, "How long to wait for the first working proxy")
	useDirect := fs.Bool("use-direct", true, "Also clone via this machine's IP alongside the proxies")
	noDirect := fs.Bool("no-direct", false, "Never use this machine's own IP (only proxies)")
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
	skip, err := loadSkipSet(*skipFile)
	if err != nil {
		return fmt.Errorf("reading -skip-file %s: %w", *skipFile, err)
	}
	var skipped []string
	urls, skipped = filterSkipped(urls, skip)
	if len(urls) == 0 {
		if len(skipped) > 0 {
			fmt.Fprintf(os.Stderr, "[*] all %d repos already in %s — nothing to scan\n", len(skipped), *skipFile)
			return nil
		}
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

	rep, err := openReportSink(reportPath, *format, *colorMode, toStdout)
	if err != nil {
		return err
	}
	defer rep.Close()

	appender := newScannedAppender(*appendScanned)
	appender.Seed(skip)

	if toStdout {
		statusf("%s", sp.dim("[*] report -> stdout (pass -out <file> to write a file)"))
	} else {
		for _, p := range rep.paths() {
			statusf("%s", sp.dim("[*] report -> "+p))
		}
	}
	statusf("%s", sp.dim(fmt.Sprintf("[*] %d repos, %d workers", len(urls), *workers)))
	if n := len(skipped); n > 0 {
		statusf("%s", sp.dim(fmt.Sprintf("[*] skipping %d already-scanned repo(s) from %s", n, *skipFile)))
	}
	if *appendScanned != "" {
		statusf("%s", sp.dim("[*] append scanned -> "+*appendScanned))
	}
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

	logf := func(f string, a ...any) { statusf(f, a...) }
	var pool *proxy.Pool
	if !*noProxy {
		pool = proxy.NewPool(*poolSize, *probePar, *verbose, *useDirect && !*noDirect, logf)
		pool.Start(ctx)
		deadline := time.Now().Add(*poolWait)
		for pool.Stats().ProxiesAlive == 0 && time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
		}
		if s := pool.Stats(); s.ProxiesAlive == 0 {
			statusf("%s", sp.yellow(fmt.Sprintf("[!] no proxy validated after %s (%d probed) — clones use direct",
				*poolWait, s.Probed)))
		} else {
			statusf("%s", sp.dim(fmt.Sprintf("[*] proxy pool ready — %d alive (clones via HTTPS_PROXY on child only)", s.ProxiesAlive)))
		}
	} else {
		statusf("%s", sp.dim("[*] -no-proxy: clones use this machine's IP only"))
	}

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
		rep.mu.Lock()
		if rep.pretty != nil {
			fmt.Fprintf(rep.pretty, "\n%s\n", rep.rp.bold("=== Interrupted ==="))
			fmt.Fprintf(rep.pretty, "  %d/%d repos completed, %d finding(s), %s elapsed\n",
				atomic.LoadInt64(&done), len(urls), atomic.LoadInt64(&findings),
				time.Since(started).Round(time.Second))
			fmt.Fprintf(rep.pretty, "  stopped early (SIGINT/SIGTERM); everything above is final\n")
		}
		rep.flush()
		rep.mu.Unlock()
		if !toStdout {
			for _, p := range rep.paths() {
				statusf("%s", sp.bold("[!] interrupted — partial report saved to "+p))
			}
		}
		cancel()
		os.Exit(130)
	}()

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
					pool: pool, verbose: *verbose, logf: logf,
				})
				atomic.AddInt64(&inFlight, -1)
				atomic.AddInt64(&done, 1)
				atomic.AddInt64(&findings, int64(len(res.Findings)))
				if res.Err != nil {
					atomic.AddInt64(&failed, 1)
				} else if err := appender.Add(repo); err != nil {
					statusf("%s", sp.yellow(fmt.Sprintf("[!] append-scanned: %v", err)))
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

				rep.writeRepo(repo, res)

				if len(res.Findings) > 0 && !toStdout && rep.pretty != nil {
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

	if pool != nil {
		s := pool.Stats()
		statusf("%s", sp.dim(fmt.Sprintf("[+] pool — %d/%d alive, %d probed, %d kept, %d requests, %d retired",
			s.Alive, *poolSize, s.Probed, s.Kept, s.Requests, s.Failures)))
	}

	elapsed := time.Since(started).Round(time.Second)
	fail := atomic.LoadInt64(&failed)

	rep.writeSummary(len(urls), fail, atomic.LoadInt64(&findings), atomic.LoadInt64(&verified), elapsed, &byDetector)

	statusf("%s", sp.bold("=== Summary ==="))
	statusf("  repos scanned %d, failed %d, findings %d (%d verified), elapsed %s",
		len(urls), fail, atomic.LoadInt64(&findings), atomic.LoadInt64(&verified), elapsed)
	statusf("%s", sp.dim(fmt.Sprintf("[+] done — %d repos, %d finding(s), %s", len(urls), atomic.LoadInt64(&findings), elapsed)))
	if !toStdout {
		for _, p := range rep.paths() {
			statusf("%s", sp.bold("[+] report -> "+p))
		}
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
	pool         *proxy.Pool
	verbose      bool
	logf         func(string, ...any)
}

// scanRepo runs trufflehog over one repo, capturing findings on stdout and
// trufflehog's own logging on stderr separately. When a proxy pool is set,
// each attempt routes the child through HTTPS_PROXY (never the parent env).
func scanRepo(ctx context.Context, bin, repo string, o scanOptions) Result {
	// --no-update: trufflehog's self-updater fails with "cannot move binary"
	// when the binary is root-owned (e.g. /usr/local/bin) and we run as a
	// normal user — which aborts the whole scan before any findings.
	args := []string{"git", repo, "--no-update"}
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
		// trufflehog --exclude-paths expects a *file* of regexes.
		// Our -exclude-paths flag is comma-separated globs → --exclude-globs.
		args = append(args, "--exclude-globs="+o.excludePaths)
	}

	res := Result{Repo: repo}
	logf := o.logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	start := time.Now()
	var stdout, stderr []byte
	var err error
	for attempt := 0; ; attempt++ {
		var pr *proxy.Proxy
		var release func()
		if o.pool != nil {
			// Short gate between acquisitions; Busy() holds the IP for the clone.
			pr = o.pool.Next(ctx, time.Second)
			if pr != nil {
				release = pr.Busy()
			}
		}
		if o.verbose {
			via := "direct"
			if pr != nil {
				via = pr.Addr()
			}
			logf("[*] %s via %s (attempt %d)", shortRepo(repo), via, attempt+1)
		}

		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = proxyChildEnv(pr)
		var errBuf strings.Builder
		cmd.Stderr = &errBuf
		stdout, err = cmd.Output()
		stderr = []byte(errBuf.String())
		if release != nil {
			release()
		}
		if err == nil {
			break
		}

		if errors.Is(err, exec.ErrNotFound) {
			res.Duration = time.Since(start)
			res.Err = fmt.Errorf("trufflehog binary %q not found in PATH", bin)
			return res
		}
		// OOM/SIGKILL is local pressure — retrying only digs the hole deeper.
		if killedBySignal(err) || permanentScanError(stderr) {
			break
		}
		if o.pool != nil && pr != nil && !pr.IsDirect() && proxyScanError(stderr, err) {
			o.pool.ReportFailure(pr)
		}
		if attempt == scanAttempts-1 {
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

// proxyChildEnv builds env for the trufflehog child: strip inherited proxy
// vars, then set HTTP(S)_PROXY only when using a pooled remote proxy.
func proxyChildEnv(pr *proxy.Proxy) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+4)
	for _, e := range base {
		key, _, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "http_proxy", "https_proxy", "all_proxy", "no_proxy":
			continue
		}
		out = append(out, e)
	}
	if u := pr.ProxyURL(); u != "" {
		out = append(out,
			"HTTP_PROXY="+u,
			"HTTPS_PROXY="+u,
			"ALL_PROXY="+u,
		)
	}
	return out
}

// proxyScanError is true when a failed clone likely implicates the proxy.
func proxyScanError(stderr []byte, err error) bool {
	s := strings.ToLower(string(stderr) + " " + err.Error())
	for _, m := range []string{
		"proxy",
		"connection reset",
		"connection refused",
		"i/o timeout",
		"tls handshake",
		"eof",
		"network is unreachable",
		"no route to host",
		"503",
		"502",
		"could not resolve",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
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

// killedBySignal reports that trufflehog did not exit on its own — usually
// SIGKILL from the OOM killer when too many full-history clones run at once.
func killedBySignal(err error) bool {
	var ee *exec.ExitError
	if err == nil || !errors.As(err, &ee) || ee.ProcessState == nil {
		return false
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return true
	}
	// Go reports -1 when the process was signaled and ExitCode is unavailable.
	return ee.ExitCode() == -1
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
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ee.ProcessState != nil {
				if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
					sig := ws.Signal()
					if sig == syscall.SIGKILL {
						return fmt.Errorf("trufflehog killed (SIGKILL — usually OOM; lower -workers)")
					}
					return fmt.Errorf("trufflehog killed (%s)", sig)
				}
				if ee.ExitCode() == -1 {
					return fmt.Errorf("trufflehog killed (signal — usually OOM; lower -workers)")
				}
				return fmt.Errorf("trufflehog exited %d", ee.ExitCode())
			}
			return fmt.Errorf("trufflehog exited with error")
		}
		return err
	}
	reason = strings.Join(strings.Fields(reason), " ")
	if len(reason) > 160 {
		reason = reason[:157] + "..."
	}
	return errors.New(reason)
}
