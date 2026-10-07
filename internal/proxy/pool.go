package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// probeURL proves a proxy can CONNECT-tunnel to GitHub over TLS. /zen
	// is the cheapest endpoint and costs no core/search quota.
	probeURL = "https://api.github.com/zen"

	// probesPerKeeper: measured yield against the search endpoint is ~4.3%,
	// so budget ~23 candidates per proxy we expect to keep.
	probesPerKeeper = 23

	// Per-IP unauthenticated GitHub limits, as pacing intervals.
	CoreInterval   = 60 * time.Second // 60 req/hr
	SearchInterval = 6 * time.Second  // 10 req/min

	DefaultPoolSize = 30
	// Validation yield is flat with concurrency, but wall time is not:
	// measured 400 candidates -> 21 kept in 39s at par=60, 22 kept in 21s
	// at par=150, 21 kept in 13s at par=300. So probe hard by default;
	// this is outbound TCP to random hosts, not GitHub traffic.
	DefaultProbePar    = 300
	probeTimeout       = 8 * time.Second
	proxyListTimeout   = 20 * time.Second
	proxyIdleTimeout   = 10 * time.Second
	maxIdleConnsPerHos = 4
)

// probeSources are public proxy lists. country=all matters: the us-only
// list yields ~0% against GitHub, the mixed lists ~4.3%.
var probeSources = []string{
	"https://api.proxyscrape.com/v4/free-proxy-list/get?request=display_proxies&proxy_format=ipport&format=text&country=all",
	"https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt",
	"https://raw.githubusercontent.com/clarketm/proxy-list/master/proxy-list-raw.txt",
}

// Proxy is a validated forward proxy with its own pacing gate, so we never
// exceed one IP's GitHub rate limit.
type Proxy struct {
	addr   string
	client *http.Client

	// direct marks this machine's own IP, which is treated as a pool member
	// so its rate-limit budget is spent alongside the proxies. It is never
	// retired: the local IP does not "die", it just gets temporarily busy.
	direct bool

	mu     sync.Mutex
	nextAt time.Time
	broken bool
	busy   bool // held across long work (git clone); Next skips until release
}

// wait blocks until this proxy's pacing interval has elapsed, then claims
// the next slot. It reports false if the proxy was retired meanwhile.
func (p *Proxy) wait(ctx context.Context, interval time.Duration) bool {
	p.mu.Lock()
	if p.broken {
		p.mu.Unlock()
		return false
	}
	now := time.Now()
	if p.nextAt.After(now) {
		delay := p.nextAt.Sub(now)
		p.nextAt = p.nextAt.Add(interval)
		p.mu.Unlock()
		select {
		case <-time.After(delay):
		case <-ctx.Done():
		}
		return true
	}
	p.nextAt = now.Add(interval)
	p.mu.Unlock()
	return true
}

func (p *Proxy) retire() {
	p.mu.Lock()
	p.broken = true
	p.mu.Unlock()
}

// Busy holds this proxy out of Pool.Next until the returned release func runs.
// Use around long-lived work (e.g. a git clone) so the same IP is not handed
// to another worker mid-transfer.
func (p *Proxy) Busy() (release func()) {
	if p == nil {
		return func() {}
	}
	p.mu.Lock()
	p.busy = true
	p.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			p.busy = false
			p.nextAt = time.Now()
			p.mu.Unlock()
		})
	}
}

// ProxyURL is the http://host:port form for subprocess env (HTTP_PROXY etc).
// Empty when this member is the local machine.
func (p *Proxy) ProxyURL() string {
	if p == nil || p.direct {
		return ""
	}
	return "http://" + p.addr
}

// PoolStats is the live health snapshot for reporting.
type PoolStats struct {
	Direct bool
	// ProxiesAlive counts validated proxies only, excluding the local machine.
	// Readiness checks must use this: including direct would make the pool look
	// ready the moment it is created and defeat the pool wait.
	ProxiesAlive int
	Target       int
	Alive        int
	Probed       int64
	Kept         int64
	Requests     int64
	Failures     int64
	Candidates   int
}

// why proxies died, for -v diagnostics.
type reasons struct {
	mu sync.Mutex
	m  map[string]int
}

func newReasons() *reasons { return &reasons{m: map[string]int{}} }

func (r *reasons) add(k string) {
	r.mu.Lock()
	r.m[k]++
	r.mu.Unlock()
}

func (r *reasons) top(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	type kv struct {
		k string
		v int
	}
	var all []kv
	total := 0
	for k, v := range r.m {
		all = append(all, kv{k, v})
		total += v
	}
	if total == 0 {
		return nil
	}
	// Simple selection sort; n is tiny.
	for i := 0; i < len(all) && i < n; i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].v > all[i].v {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	out := make([]string, 0, len(all))
	for _, e := range all {
		out = append(out, fmt.Sprintf("%s=%d (%.0f%%)", e.k, e.v, 100*float64(e.v)/float64(total)))
	}
	return out
}

// Pool validates proxies in the background and hands out working ones.
// Free proxies are ephemeral (~58% of a pool dies within 45s), so this
// continuously replenishes rather than fetching once.
type Pool struct {
	target  int
	par     int
	verbose bool
	logf    func(string, ...any)

	mu     sync.Mutex
	live   []*Proxy
	cand   []string
	candAt time.Time

	lastWaitNotice time.Time

	direct bool

	probed   int64
	kept     int64
	requests int64
	failures int64

	why *reasons
}

// NewPool builds a pool that replenishes toward target size.
func NewPool(target, par int, verbose, useDirect bool, logf func(string, ...any)) *Pool {
	if target < 1 {
		target = 1
	}
	if par < 1 {
		par = 1
	}
	p := &Pool{target: target, par: par, verbose: verbose, logf: logf, why: newReasons()}
	if useDirect {
		// Seeded straight into the live set: it needs no validation and is
		// available immediately, so the very first request can go direct
		// instead of waiting ~3s for the first proxy.
		p.live = append(p.live, newDirectProxy())
		p.direct = true
	}
	return p
}

// Stats returns a snapshot.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	proxies := 0
	for _, pr := range p.live {
		pr.mu.Lock()
		broken := pr.broken
		pr.mu.Unlock()
		if !broken && !pr.direct {
			proxies++
		}
	}
	return PoolStats{
		Direct:       p.direct,
		ProxiesAlive: proxies,
		Target:       p.target,
		Alive:        len(p.live),
		Probed:       atomic.LoadInt64(&p.probed),
		Kept:         atomic.LoadInt64(&p.kept),
		Requests:     atomic.LoadInt64(&p.requests),
		Failures:     atomic.LoadInt64(&p.failures),
		Candidates:   len(p.cand),
	}
}

// Start launches the background replenisher. Call Stop to end it.
func (p *Pool) Start(ctx context.Context) {
	go p.replenish(ctx)
}

// Stop is a no-op; cancellation of ctx ends the replenisher.
func (p *Pool) Stop() {}

// Next returns a proxy ready to use, waiting for its pacing gate. It
// returns nil when the pool has nothing left to give, which signals the
// caller to fall back to a direct connection.
// Next returns a proxy that is allowed to make a request now, waiting if
// every live proxy is still inside its per-IP cooldown. When it has to wait,
// it says so (throttled) — a silent multi-second block looks identical to a
// hung run, which is exactly what it is not.
func (p *Pool) Next(ctx context.Context, interval time.Duration) *Proxy {
	deadline := time.Now().Add(30 * time.Second)
	waitingSince := time.Time{}
	for {
		p.mu.Lock()
		if len(p.live) == 0 {
			p.mu.Unlock()
			return nil
		}
		// Prefer the proxy that has been idle longest, but keep this
		// machine's own IP strictly as a filler: it only wins when no proxy
		// is ready. Letting it win ties would spend the local rate-limit
		// budget on most requests, which is the one budget we cannot replace
		// -- a proxy we exhaust is free, a 403 here also blocks the user's
		// other GitHub work.
		var best, bestDirect *Proxy
		var bestAt, bestDirectAt time.Time
		for _, pr := range p.live {
			pr.mu.Lock()
			at := pr.nextAt
			broken := pr.broken
			busy := pr.busy
			pr.mu.Unlock()
			if broken || busy {
				continue
			}
			if pr.direct {
				if bestDirect == nil || at.Before(bestDirectAt) {
					bestDirect, bestDirectAt = pr, at
				}
				continue
			}
			if best == nil || at.Before(bestAt) {
				best, bestAt = pr, at
			}
		}
		// Direct is used only when it is strictly available sooner than the
		// best proxy. Requiring "strictly" keeps it a filler: on a tie the
		// proxy wins, which is what protects the local rate-limit budget.
		// Without this, a proxy paced an hour out would block the caller for
		// an hour even though the local IP was sitting idle.
		if best == nil || (bestDirect != nil && bestDirectAt.Before(bestAt)) {
			best, bestAt = bestDirect, bestDirectAt
		}
		if best == nil {
			// No free member: either every live proxy is Busy, or every
			// entry is retired. Busy members must stay in the set — wiping
			// them would strand a git clone that still holds the release.
			alive := 0
			for _, pr := range p.live {
				pr.mu.Lock()
				if !pr.broken {
					alive++
				}
				pr.mu.Unlock()
			}
			if alive == 0 {
				p.live = nil
				p.mu.Unlock()
				return nil
			}
			p.mu.Unlock()
			if waitingSince.IsZero() {
				waitingSince = time.Now()
			}
			p.noteWaiting(alive, time.Since(waitingSince).Round(time.Second))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(50 * time.Millisecond):
			}
			if time.Now().After(deadline) {
				return nil
			}
			continue
		}
		alive := len(p.live)
		p.mu.Unlock()

		if bestAt.After(time.Now()) {
			if waitingSince.IsZero() {
				waitingSince = time.Now()
			}
			// One notice per wait, not one per loop iteration.
			p.noteWaiting(alive, time.Since(waitingSince).Round(time.Second))
		}

		// wait() sleeps until this member's slot, so the loop deadline only
		// means anything if it is in the context passed down.
		waitCtx, cancel := context.WithDeadline(ctx, deadline)
		ok := best.wait(waitCtx, interval)
		cancel()
		if ok {
			atomic.AddInt64(&p.requests, 1)
			return best
		}
		// wait() failed, so it was retired. Drop it and try again.
		p.drop(best)
		if time.Now().After(deadline) {
			return nil
		}
	}
}

func (p *Pool) drop(pr *Proxy) {
	if pr.direct {
		return
	}
	pr.retire()
	p.mu.Lock()
	out := p.live[:0]
	for _, x := range p.live {
		if x != pr {
			out = append(out, x)
		}
	}
	p.live = out
	p.mu.Unlock()
}

// ReportFailure retires a proxy whose request failed at the transport level.
func (p *Pool) ReportFailure(pr *Proxy) {
	atomic.AddInt64(&p.failures, 1)
	if pr.direct {
		// Retire only for this pacing interval. Marking the local IP broken
		// would remove the one source that is guaranteed to exist, which is
		// exactly backwards.
		pr.backoff(intervalDirectBackoff)
		return
	}
	p.drop(pr)
	pr.close()
}

// intervalDirectBackoff is how long the local IP is skipped after a failure.
const intervalDirectBackoff = 90 * time.Second

// backoff pushes this proxy's next-available time out without retiring it.
func (p *Proxy) backoff(d time.Duration) {
	p.mu.Lock()
	at := time.Now().Add(d)
	if at.After(p.nextAt) {
		p.nextAt = at
	}
	p.mu.Unlock()
}

// noteWaiting explains a block on pool exhaustion, at most every 10s.
func (p *Pool) noteWaiting(alive int, d time.Duration) {
	p.mu.Lock()
	if time.Since(p.lastWaitNotice) < 10*time.Second {
		p.mu.Unlock()
		return
	}
	p.lastWaitNotice = time.Now()
	p.mu.Unlock()
	p.logf("[*] all %d proxies are inside their per-IP cooldown, waiting %s for one to free up",
		alive, d)
}

// close releases the proxy's idle sockets. Retired proxies are ~58% of the
// pool and each held up to maxIdleConnsPerHos keep-alive connections, so
// without this the process exhausts its own file descriptors — which surfaces
// as "Too many open connections" against otherwise healthy proxies.
func (pr *Proxy) close() {
	if tr, ok := pr.client.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

// replenish keeps the pool near its target size.
func (p *Pool) replenish(ctx context.Context) {
	for ctx.Err() == nil {
		p.mu.Lock()
		need := p.target - len(p.live)
		p.mu.Unlock()

		if need <= 0 {
			sleepCtx(ctx, 5*time.Second)
			continue
		}

		batch := p.takeBatch(need * probesPerKeeper)
		if len(batch) == 0 {
			// Candidates exhausted: reload the lists.
			if err := p.loadCandidates(ctx); err != nil {
				p.logf("[!] proxy list fetch failed: %v", err)
				sleepCtx(ctx, 15*time.Second)
			}
			continue
		}

		p.probeBatch(ctx, batch)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

// takeBatch pulls probe candidates off the queue, refilling if stale.
func (p *Pool) takeBatch(n int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.cand) == 0 && time.Since(p.candAt) > time.Minute {
		return nil // signal: needs a reload
	}
	if n > len(p.cand) {
		n = len(p.cand)
	}
	if n == 0 {
		return nil
	}
	batch := make([]string, n)
	copy(batch, p.cand[:n])
	p.cand = p.cand[n:]
	return batch
}

// probeBatch validates candidates concurrently and keeps the survivors.
//
// Keepers are banked the moment they validate, not after the whole batch
// drains. Most candidates die slowly (72% hit the probe timeout), so waiting
// for a full batch meant a 276-candidate pass blocked ~40s with an empty
// pool even though working proxies had already been found. Banking as we go
// lets waitFor return on the first success instead of the last.
func (p *Pool) probeBatch(ctx context.Context, batch []string) {
	sem := make(chan struct{}, p.par)
	var wg sync.WaitGroup

	for _, addr := range batch {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(addr string) {
			defer wg.Done()
			defer func() { <-sem }()

			atomic.AddInt64(&p.probed, 1)
			ok, why := probeOne(ctx, addr)
			if !ok {
				p.why.add(why)
				return
			}

			p.mu.Lock()
			full := len(p.live) >= p.target
			if !full {
				p.live = append(p.live, newProxy(addr))
				atomic.AddInt64(&p.kept, 1)
			}
			alive := len(p.live)
			p.mu.Unlock()

			if !full {
				p.logf("[+] proxy validated: %s (%d/%d alive)", addr, alive, p.target)
			}
		}(addr)
	}
	wg.Wait()

	s := p.Stats()
	if len(batch) > 0 {
		p.logf("[*] probed %d, %d/%d alive, %d kept, yield %.1f%%",
			len(batch), s.Alive, p.target, s.Kept,
			100*float64(s.Kept)/float64(s.Probed))
	}
	if p.verbose {
		if top := p.why.top(6); top != nil {
			p.logf("[*] probe failures: %s", strings.Join(top, " "))
		}
	}
}

// probeOne reports whether a candidate can reach GitHub over TLS, and if
// not, why — so -v can explain a dry pool instead of leaving us guessing.
func probeOne(ctx context.Context, addr string) (bool, string) {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	pr := newProxy(addr)
	req, err := http.NewRequestWithContext(pctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return false, "badreq"
	}
	resp, err := pr.client.Do(req)
	if err != nil {
		return false, classify(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusOK {
		return true, ""
	}
	return false, "status" + strconv.Itoa(resp.StatusCode)
}

// classify buckets a transport error into a human-readable reason.
func classify(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"):
		return "refused"
	case strings.Contains(s, "connection reset"):
		return "reset"
	case strings.Contains(s, "EOF"):
		return "eof"
	case strings.Contains(s, "malformed HTTP"):
		return "nothttp"
	case strings.Contains(s, "407"), strings.Contains(s, "403"):
		return "blocked"
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline exceeded"):
		return "timeout"
	case strings.Contains(s, "certificate"), strings.Contains(s, "tls"):
		return "tls"
	case strings.Contains(s, "no such host"):
		return "dns"
	case strings.Contains(s, "proxyconnect"):
		return "connect"
	}
	return "other"
}

// newDirectProxy wraps an ordinary client so the local machine participates in
// the pool under the same pacing rules. Using it alongside the proxies is the
// cheapest speedup available: it is one extra source of requests that is
// guaranteed to exist and needs no validation.
func newDirectProxy() *Proxy {
	return &Proxy{
		addr:   "direct (this machine)",
		direct: true,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConnsPerHost: maxIdleConnsPerHos,
				IdleConnTimeout:     proxyIdleTimeout,
			},
			Timeout: 15 * time.Second,
		},
	}
}

func newProxy(addr string) *Proxy {
	u, _ := url.Parse("http://" + addr)
	return &Proxy{
		addr: addr,
		client: &http.Client{
			Transport: &http.Transport{
				Proxy:               http.ProxyURL(u),
				MaxIdleConnsPerHost: maxIdleConnsPerHos,
				IdleConnTimeout:     proxyIdleTimeout,
			},
			Timeout: 15 * time.Second,
		},
	}
}

// loadCandidates pulls and dedupes every source into the candidate queue.
func (p *Pool) loadCandidates(ctx context.Context) error {
	client := &http.Client{Timeout: proxyListTimeout}

	var mu sync.Mutex
	seen := map[string]bool{}
	var all []string

	var wg sync.WaitGroup
	for _, src := range probeSources {
		wg.Add(1)
		go func(src string) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, line := range strings.Split(string(body), "\n") {
				addr := strings.TrimSpace(line)
				if !validAddr(addr) || seen[addr] {
					continue
				}
				seen[addr] = true
				all = append(all, addr)
			}
		}(src)
	}
	wg.Wait()

	if len(all) == 0 {
		return fmt.Errorf("no candidates from %d sources", len(probeSources))
	}

	p.mu.Lock()
	p.cand = append(all, p.cand...)
	p.candAt = time.Now()
	n := len(p.cand)
	p.mu.Unlock()

	p.logf("[+] loaded %d proxy candidates", n)
	return nil
}

// validAddr keeps only host:port pairs.
func validAddr(s string) bool {
	host, port, err := net.SplitHostPort(s)
	return err == nil && host != "" && port != ""
}

// DirectClient is the fallback when the pool can't supply anything.
func DirectClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{MaxIdleConnsPerHost: maxIdleConnsPerHos},
		Timeout:   15 * time.Second,
	}
}

// Client returns the HTTP client bound to this pool member. The caller owns
// pacing: obtain the Proxy from Pool.Next, which has already reserved a slot.
func (p *Proxy) Client() *http.Client { return p.client }

// Addr is how this pool member is shown in diagnostics. For the local machine
// it is a description rather than a host:port.
func (p *Proxy) Addr() string { return p.addr }

// IsDirect reports whether this pool member is the local machine rather than a
// proxy, which callers use to avoid describing it as a remote address.
func (p *Proxy) IsDirect() bool { return p.direct }
