package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/adamsiwiec/truffles/internal/proxy"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	perPage    = 100
	flushEvery = 10
	maxResults = 10000

	// maxPagesPerSource is a runaway guard for owner enumeration:
	// 1000 pages = 100k repos.
	maxPagesPerSource = 1000

	// maxSearchPages is the real ceiling for a global search. GitHub only
	// exposes the first 1000 results, so 10 pages at per_page=100 is the
	// maximum. Requesting page 11 returns 422 "Only the first 1000 search
	// results are available" — clamping to maxPagesPerSource instead
	// queued hundreds of pages that all 422'd and killed the run.
	maxSearchPages = 10
)

// searchSrc is the pseudo-source key used when searching globally.
const searchSrc = "\x00global-search"

// srcName is a human-readable label for logging.
func srcName(s srcSpec) string {
	if s.query != "" {
		return "q=" + s.query
	}
	return s.key
}

// srcSpec is one unit of enumeration: either a GitHub owner, or a single
// global search query. Global mode creates one of these per query so that
// -workers has real work to hand out, each page through a different proxy.
type srcSpec struct {
	key   string // owner name, or searchSrc
	query string // non-empty => global search
}

type Repo struct {
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

// statusErr marks whether retrying through another proxy could help.
type statusErr struct {
	retryable bool
	msg       string
}

func (e *statusErr) Error() string { return e.msg }

// isTerminal reports whether an error is one no amount of retrying fixes.
func isTerminal(err error) bool {
	var se *statusErr
	return errors.As(err, &se) && !se.retryable
}

// doGitHub issues an authenticated GET and maps failures onto statusErr.
func doGitHub(client *http.Client, apiURL, token, notFoundMsg string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return nil, &statusErr{true, fmt.Sprintf("rate limited (%d) — pass -token to raise the limit", resp.StatusCode)}
	}
	if resp.StatusCode == 404 {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return nil, &statusErr{false, notFoundMsg}
	}
	if resp.StatusCode == 422 {
		// GitHub returns this once you page past result 1000. It means
		// "there are no more results", not "the request was malformed",
		// so it must not abort the whole run the way a fatal 4xx does.
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &statusErr{false, fmt.Sprintf("past github's 1000-result search cap: %s",
			strings.TrimSpace(string(body)))}
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &statusErr{resp.StatusCode >= 500,
			fmt.Sprintf("github %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
	return resp, nil
}

// lastPageFromLink extracts the page number from a rel="last" Link header.
// GitHub omits the header entirely when there is only one page.
func lastPageFromLink(header string) int {
	for _, part := range strings.Split(header, ",") {
		if !strings.Contains(part, `rel="last"`) {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(part[strings.Index(part, "<")+1 : strings.Index(part, ">")]))
		if err != nil {
			continue
		}
		if p, err := strconv.Atoi(u.Query().Get("page")); err == nil {
			return p
		}
	}
	return 0
}

// fetchPage reads one page of an owner's repos, plus the last page index.
func fetchPage(client *http.Client, owner string, page int, token string) ([]Repo, int, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/repos?per_page=%d&page=%d",
		url.PathEscape(owner), perPage, page)
	resp, err := doGitHub(client, apiURL, token, fmt.Sprintf("no such user or org %q", owner))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	last := lastPageFromLink(resp.Header.Get("Link"))
	if last == 0 && page == 1 {
		last = 1 // no Link header => single page
	}

	var repos []Repo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, 0, err
	}
	return repos, last, nil
}

type searchResult struct {
	TotalCount int    `json:"total_count"`
	Items      []Repo `json:"items"`
}

// fetchSearchPage queries the repo search index, which needs no owner.
func fetchSearchPage(client *http.Client, query string, page int, token string) ([]Repo, int, error) {
	apiURL := fmt.Sprintf("https://api.github.com/search/repositories?q=%s&per_page=%d&page=%d",
		url.QueryEscape(query), perPage, page)
	resp, err := doGitHub(client, apiURL, token, "search failed")
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var sr searchResult
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, 0, err
	}

	total := (sr.TotalCount + perPage - 1) / perPage // ceil
	if total == 0 {
		total = 1
	}
	return sr.Items, total, nil
}

// multiFlag collects a repeatable flag into a slice.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*m = append(*m, part)
		}
	}
	return nil
}

// matchAny reports whether name matches at least one glob (union).
func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, err := path.Match(p, name); err == nil && ok {
			return true
		}
	}
	return false
}

// buildMatcher compiles patterns into a name filter. A repo is kept if it
// matches ANY pattern (union). No patterns means keep everything.
func buildMatcher(patterns []string, useRegex bool) (func(string) bool, error) {
	if len(patterns) == 0 {
		return func(string) bool { return true }, nil
	}

	if !useRegex {
		for _, p := range patterns {
			if _, err := path.Match(p, "test"); err != nil {
				return nil, fmt.Errorf("invalid pattern %q: %v", p, err)
			}
		}
		return func(name string) bool { return matchAny(patterns, name) }, nil
	}

	res := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %v", p, err)
		}
		res = append(res, re)
	}
	return func(name string) bool {
		for _, re := range res {
			if re.MatchString(name) {
				return true
			}
		}
		return false
	}, nil
}

// deriveQuery turns a pattern into a search-index query string. GitHub's
// search API takes plain terms, not globs or regexes.
func deriveQuery(patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	best := ""
	for _, chunk := range strings.FieldsFunc(patterns[0], func(r rune) bool {
		return strings.ContainsRune("*?[](){}|^$\\+", r)
	}) {
		if len(chunk) > len(best) {
			best = chunk
		}
	}
	return best
}

func runSearch(args []string) error {
	fs := newFlagSet("search")
	out := fs.String("out", "", "Write results to file (default stdout)")
	limit := fs.Int("limit", 0, fmt.Sprintf("Max results to output, max %d (0 = no limit)", maxResults))
	retries := fs.Int("retries", 4, "Attempts per page before giving up")
	noProxy := fs.Bool("no-proxy", false, "Connect directly, skipping the proxy pool")
	token := fs.String("token", "", "GitHub token (raises rate limits)")
	useRegex := fs.Bool("regex", false, "Treat patterns as regexes instead of globs")
	poolSize := fs.Int("pool-size", proxy.DefaultPoolSize, "Target number of validated proxies")
	probePar := fs.Int("probe-par", proxy.DefaultProbePar, "Concurrent proxy validation probes")
	poolWait := fs.Duration("pool-wait", 60*time.Second, "How long to wait for the first working proxy")
	workers := fs.Int("workers", 8, "Concurrent page fetches")
	verbose := fs.Bool("v", false, "Verbose: log every probe and every request")
	useDirect := fs.Bool("use-direct", true, "Also spend this machine's own rate-limit budget alongside the proxies")
	noDirect := fs.Bool("no-direct", false, "Never use this machine's own IP (only proxies)")
	progressEvery := fs.Duration("progress", 10*time.Second, "Log progress this often; 0 disables the heartbeat")
	filter := fs.String("filter", "", "Extra local glob/regex filter applied in global mode")
	var queries multiFlag
	fs.Var(&queries, "q", "GitHub search query; repeatable. Without -owner, positionals are used as queries")
	var owners multiFlag
	fs.Var(&owners, "owner", "GitHub user/org (repeatable, comma-separated). Omit to search globally")
	if err := parseFlags(fs, args); err != nil {
		// `truffles search -h` prints usage and succeeds; it is not an error.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	// Positional args are always patterns; owner comes only from -owner.
	patterns := fs.Args()

	if len(owners) == 0 && len(patterns) == 0 && len(queries) == 0 {
		return fmt.Errorf("nothing to search — pass -owner, a pattern, or -q\n" +
			"  e.g. search -owner BurntSushi '*llm*'\n" +
			"  e.g. search 'llm' -limit 100")
	}

	// In owner mode a pattern is a local filter over that owner's repos.
	// In global mode a pattern IS the search query — the index already
	// filtered, so no local filter is applied unless -filter is given.
	// Otherwise `search llm` would only match repos named exactly "llm".
	globalMode := len(owners) == 0
	filterPatterns := patterns
	if globalMode {
		filterPatterns = nil
		if *filter != "" {
			filterPatterns = strings.Split(*filter, ",")
		}
	}

	matches, err := buildMatcher(filterPatterns, *useRegex)
	if err != nil {
		return err
	}

	// Global queries: explicit -q wins, else one query per positional.
	var searchQueries []string
	if globalMode {
		searchQueries = queries
		if len(searchQueries) == 0 {
			for _, p := range patterns {
				if t := deriveQuery([]string{p}); t != "" {
					searchQueries = append(searchQueries, t)
				} else if *useRegex {
					return fmt.Errorf("cannot derive a search query from regex %q: pass -q <term>", p)
				}
			}
		}
		if len(searchQueries) == 0 {
			return fmt.Errorf("no usable search query: pass -q <term>")
		}
		if *token == "" {
			fmt.Fprintf(os.Stderr, "[!] repo search is 10 req/min per IP unauthenticated — pass -token\n")
		}
	}

	if *limit < 0 {
		return fmt.Errorf("-limit must be >= 0 (got %d)", *limit)
	}
	if *retries < 1 {
		*retries = 1
	}
	if *limit > maxResults {
		fmt.Fprintf(os.Stderr, "[!] -limit %d exceeds max %d, clamping\n", *limit, maxResults)
		*limit = maxResults
	}

	dest := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		dest = f
		fmt.Fprintf(os.Stderr, "[+] writing to %s\n", *out)
	}

	// Flush every flushEvery lines so partial results survive a kill or hang.
	buf := bufio.NewWriter(dest)
	defer buf.Flush()

	logf := func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pool *proxy.Pool
	if !*noProxy {
		pool = proxy.NewPool(*poolSize, *probePar, *verbose, *useDirect && !*noDirect, logf)
		pool.Start(ctx)

		// Free proxies are ~4% yield, so wait for the first keeper rather
		// than silently burning the rate limit on a direct connection.
		deadline := time.Now().Add(*poolWait)
		for pool.Stats().ProxiesAlive == 0 && time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
		}
		if s := pool.Stats(); s.ProxiesAlive == 0 {
			logf("[!] no proxy validated after %s (%d probed) — using direct",
				*poolWait, s.Probed)
		}
	}

	// getClient returns a client plus the proxy behind it (nil = direct).
	getClient := func(interval time.Duration) (*http.Client, *proxy.Proxy) {
		if pool != nil {
			if pr := pool.Next(ctx, interval); pr != nil {
				return pr.Client(), pr
			}
		}
		return proxy.DirectClient(), nil
	}

	via := func(pr *proxy.Proxy) string {
		if pr == nil {
			return "direct"
		}
		return pr.Addr()
	}

	// A dead proxy is retired so the pool stops handing it out.
	withRetries := func(interval time.Duration, fn func(*http.Client) ([]Repo, int, error)) ([]Repo, int, string, error) {
		var lastErr error
		last := "direct"
		for attempt := 0; attempt < *retries; attempt++ {
			client, pr := getClient(interval)
			last = via(pr)
			repos, total, err := fn(client)
			if err == nil {
				return repos, total, last, nil
			}
			lastErr = err

			if pr != nil && !errors.As(err, new(*statusErr)) {
				pool.ReportFailure(pr)
			}
			if isTerminal(err) {
				return nil, 0, last, err
			}
			if attempt < *retries-1 {
				logf("[!] via %s: %v — retry %d/%d", last, err, attempt+1, *retries)
			}
		}
		return nil, 0, last, lastErr
	}

	// Fetch one page of one source. Global search paces at 10/min per IP,
	// owner enumeration at 60/hr per IP.
	fetchPageOf := func(src srcSpec, page int) ([]Repo, int, string, error) {
		if src.query != "" {
			return withRetries(proxy.SearchInterval, func(c *http.Client) ([]Repo, int, error) {
				return fetchSearchPage(c, src.query, page, *token)
			})
		}
		return withRetries(proxy.CoreInterval, func(c *http.Client) ([]Repo, int, error) {
			return fetchPage(c, src.key, page, *token)
		})
	}

	var (
		emitMu    sync.Mutex
		matched   atomic.Int64
		calls     atomic.Int64
		truncated bool
		start     = time.Now()
	)

	// poolDesc describes where requests are currently going.
	poolDesc := func() string {
		if pool == nil {
			return "direct"
		}
		s := pool.Stats()
		if s.Alive == 0 {
			return "0 alive (direct)"
		}
		desc := fmt.Sprintf("%d/%d alive", s.Alive, s.Target)
		if s.Direct {
			desc += " +direct"
		}
		return desc
	}

	// Progress reporting. Without this a 90s run looks exactly like a hang:
	// the only count used to appear in the final line, so there was no way to
	// tell "finding nothing" from "stuck".
	//
	// Reports are throttled: one page can yield 100 repos, so reporting every
	// flush produced a burst of identical lines. rate limits to one line per
	// reportMinGap; the heartbeat still fires on its own schedule.
	var (
		reportMu     sync.Mutex
		lastReport   time.Time
		reportMinGap = 2 * time.Second
	)

	reportProgress := func(stalled bool) {
		n, c := matched.Load(), calls.Load()
		el := time.Since(start).Round(time.Second)

		reportMu.Lock()
		now := time.Now()
		if !now.After(lastReport.Add(reportMinGap)) {
			reportMu.Unlock()
			return
		}
		lastReport = now
		reportMu.Unlock()

		if stalled {
			logf("[*] %s — %d repos, %d calls, pool %s (no new results yet)",
				el, n, c, poolDesc())
			return
		}
		logf("[+] %s — %d repos found, %d calls, pool %s", el, n, c, poolDesc())
	}

	// Heartbeat: proves the run is alive even when nothing is being emitted.
	if *progressEvery > 0 {
		var lastMatched, lastCalls int64
		stopHB := make(chan struct{})
		var hbWG sync.WaitGroup
		hbWG.Add(1)
		go func() {
			defer hbWG.Done()
			t := time.NewTicker(*progressEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-stopHB:
					return
				case <-t.C:
					m, c := matched.Load(), calls.Load()
					reportProgress(m == lastMatched && c == lastCalls)
					lastMatched, lastCalls = m, c
				}
			}
		}()
		defer func() {
			close(stopHB)
			hbWG.Wait()
		}()
	}

	emit := func(repos []Repo) {
		for _, r := range repos {
			if *limit > 0 && matched.Load() >= int64(*limit) {
				truncated = true
				return
			}
			if matches(r.Name) {
				fmt.Fprintln(buf, r.HTMLURL)
				n := matched.Add(1)
				if n%flushEvery == 0 {
					buf.Flush()
					reportProgress(false)
				}
			}
		}
	}

	// Sources to walk: each owner, or the search index as a single source.
	var sources []srcSpec
	if globalMode {
		// One source per query: this is what gives -workers real work and
		// spreads the 10 req/min limit across distinct proxy IPs.
		for i, q := range searchQueries {
			sources = append(sources, srcSpec{key: fmt.Sprintf("%s#%d", searchSrc, i), query: q})
		}
	} else {
		for _, o := range owners {
			sources = append(sources, srcSpec{key: o})
		}
	}

	// Phase 1: fetch page 1 of each source to learn how many pages exist.
	// GitHub reports this in the Link header (rel="last") and total_count,
	// so we never issue speculative requests past the end.
	//
	// Discovery is retried across rounds too: a single dead proxy on page 1
	// would otherwise silently drop that org's entire repo list.
	var (
		fatal   error
		failMu  sync.Mutex
		work    = map[srcSpec][]int{}
		failedO = map[string]bool{}
	)

	pending := sources
	for round := 0; round < 3 && len(pending) > 0; round++ {
		if round > 0 {
			logf("[*] retrying discovery for %d source(s), round %d", len(pending), round)
			if pool != nil {
				deadline := time.Now().Add(30 * time.Second)
				for pool.Stats().ProxiesAlive == 0 && time.Now().Before(deadline) {
					time.Sleep(500 * time.Millisecond)
				}
			}
		}

		var stillPending []srcSpec
		for _, src := range pending {
			repos, totalPages, via1, err := fetchPageOf(src, 1)
			if err != nil {
				if isTerminal(err) {
					return err
				}
				stillPending = append(stillPending, src)
				continue
			}

			calls.Add(1)
			if src.query != "" {
				logf("[*] query %q — %d results indexed, %d page(s) (max 1000 reachable), page 1 via %s",
					src.query, totalPages*perPage, totalPages, via1)
			} else {
				logf("[*] enumerating %s (%d pages, first page via %s)", src.key, totalPages, via1)
			}

			emit(repos)
			buf.Flush()

			if truncated {
				continue
			}
			cap := maxPagesPerSource
			if src.query != "" {
				cap = maxSearchPages
			}
			if totalPages > cap {
				logf("[*] clamping %s from %d to %d page(s)", srcName(src), totalPages, cap)
				totalPages = cap
			}
			for p := 2; p <= totalPages; p++ {
				work[src] = append(work[src], p)
			}
		}
		pending = stillPending
	}
	for _, src := range pending {
		failedO[src.key] = true
	}

	// Phase 2: fan out the remaining known pages across workers.
	type job struct {
		src  srcSpec
		page int
	}

	var (
		failedMu sync.Mutex
		failed   []job // retried in later rounds, never silently dropped
		dead     []job // gave up entirely; reported to the user
	)

	nWorkers := *workers
	if nWorkers < 1 {
		nWorkers = 1
	}

	// runPass pushes every queued page through fetchPageOf, retrying the
	// ones that fail across rounds with freshly validated proxies.
	runPass := func(queued []job) {
		ch := make(chan job, 64)
		// Feed from a separate goroutine: pushing inline would block forever
		// once the buffer filled, because no worker is running yet. That
		// deadlocked any source with more than one buffer's worth of pages
		// (a global search easily queues 1000).
		go func() {
			defer close(ch)
			for _, j := range queued {
				select {
				case ch <- j:
				case <-ctx.Done():
					return
				}
			}
		}()

		var wg sync.WaitGroup
		for i := 0; i < nWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range ch {
					// Stop pulling new pages once -limit is satisfied;
					// otherwise we'd burn requests on results we drop.
					if ctx.Err() != nil {
						return
					}
					repos, _, lastVia, err := fetchPageOf(j.src, j.page)
					if err != nil {
						if isTerminal(err) {
							failMu.Lock()
							if fatal == nil {
								fatal = err
							}
							failMu.Unlock()
							return
						}
						failedMu.Lock()
						failed = append(failed, j)
						failedMu.Unlock()
						continue
					}
					emitMu.Lock()
					calls.Add(1)
					if *verbose {
						name := j.src.key
						if j.src.query != "" {
							name = "q=" + j.src.query
						}
						logf("[*] %s page %d -> %d repos via %s", name, j.page, len(repos), lastVia)
					}
					emit(repos)
					buf.Flush()
					emitMu.Unlock()
				}
			}()
		}
		wg.Wait()
	}

	queued := make([]job, 0)
	for src, pages := range work {
		for _, p := range pages {
			queued = append(queued, job{src, p})
		}
	}
	runPass(queued)

	// Retry rounds. Free proxies die constantly, so a page that failed
	// against a dead proxy often succeeds against a fresh one. Dropping it
	// would silently lose repos.
	for round := 0; round < 3 && len(failed) > 0; round++ {
		retry := failed
		failed = nil
		logf("[*] retrying %d failed page(s), round %d", len(retry), round+1)

		// Give the pool a moment to validate replacements.
		if pool != nil {
			deadline := time.Now().Add(30 * time.Second)
			for pool.Stats().ProxiesAlive == 0 && time.Now().Before(deadline) {
				time.Sleep(500 * time.Millisecond)
			}
		}
		runPass(retry)
	}
	dead = append(dead, failed...)
	buf.Flush()

	if fatal != nil {
		return fatal
	}

	// Loud, explicit accounting for anything we could not retrieve.
	if len(dead) > 0 {
		pages := make([]string, 0, len(dead))
		for _, j := range dead {
			pages = append(pages, fmt.Sprintf("%s p%d", j.src.key, j.page))
		}
		logf("[!] INCOMPLETE — %d page(s) failed after retries: %s",
			len(dead), strings.Join(pages, ", "))
		logf("[!] results are partial; re-run to fill the gaps")
	}

	if len(owners) > 0 {
		for _, o := range owners {
			if failedO[o] {
				logf("[!] %s was not fully enumerated", o)
			}
		}
	}

	note := ""
	if truncated {
		note = fmt.Sprintf(" (stopped at -limit %d)", *limit)
	}
	label := strings.Join(patterns, " ")
	if *useRegex {
		label = "regex:" + label
	}
	fmt.Fprintf(os.Stderr, "[+] done — %d calls, %d matched %s%s\n",
		calls.Load(), matched.Load(), label, note)

	if pool != nil {
		s := pool.Stats()
		fmt.Fprintf(os.Stderr,
			"[+] pool — %d/%d alive, %d probed, %d kept, %d requests, %d retired\n",
			s.Alive, *poolSize, s.Probed, s.Kept, s.Requests, s.Failures)
		if s.Kept > 0 {
			fmt.Fprintf(os.Stderr, "[+] proxy yield %.1f%%\n", 100*float64(s.Kept)/float64(s.Probed))
		}
	}

	p := palette{on: resolveColor("auto", os.Stderr)}
	outHint := *out
	if outHint == "" || outHint == "-" {
		outHint = "repos.txt"
	}
	p.printNext(os.Stderr,
		fmt.Sprintf("truffles scan -f %s", outHint),
		fmt.Sprintf("truffles scan -f %s -format csv", outHint),
		"truffles playbook -f playbook.yaml",
	)
	return nil
}
