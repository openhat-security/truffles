package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/adamsiwiec/truffles/internal/proxy"
)

const (
	performanceSchemaVersion = 1
	performanceBasename      = "performance.json"
	maxPerformanceEvents     = 25000
)

// PerformanceMetadata is written first in performance.json (stable top-level block).
type PerformanceMetadata struct {
	SchemaVersion int       `json:"schema_version"`
	Playbook      string    `json:"playbook,omitempty"`
	Command       string    `json:"command,omitempty"`
	CollectDir    string    `json:"collect_dir,omitempty"`
	Output        string    `json:"output,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	Hostname      string    `json:"hostname,omitempty"`
}

type performancePhaseSummary struct {
	Duration        string  `json:"duration"`
	DurationSeconds float64 `json:"duration_seconds"`
	Skipped         bool    `json:"skipped,omitempty"`
}

type performanceSearchSummary struct {
	performancePhaseSummary
	Matched      int64 `json:"matched_repos,omitempty"`
	Calls        int64 `json:"api_calls,omitempty"`
	SkippedKnown int64 `json:"skipped_known,omitempty"`
	FailedPages  int   `json:"failed_pages,omitempty"`
	Incomplete   bool  `json:"incomplete,omitempty"`
}

type performanceScanSummary struct {
	performancePhaseSummary
	Repos        int     `json:"repos,omitempty"`
	Failed       int64   `json:"failed,omitempty"`
	Findings     int64   `json:"findings,omitempty"`
	Verified     int64   `json:"verified,omitempty"`
	ReposPerHour float64 `json:"repos_per_hour,omitempty"`
	P50RepoSec   float64 `json:"p50_repo_seconds,omitempty"`
	P95RepoSec   float64 `json:"p95_repo_seconds,omitempty"`
	MaxRepoSec   float64 `json:"max_repo_seconds,omitempty"`
}

type performanceCollectSummary struct {
	performancePhaseSummary
	FilesCopied int `json:"files_copied,omitempty"`
}

type performanceProxySummary struct {
	Alive        int     `json:"alive"`
	ProxiesAlive int     `json:"proxies_alive"`
	ReserveAlive int     `json:"reserve_alive"`
	Probed       int64   `json:"probed"`
	Kept         int64   `json:"kept"`
	Requests     int64   `json:"requests"`
	Failures     int64   `json:"failures"`
	YieldPercent float64 `json:"yield_percent,omitempty"`
}

type performanceSummary struct {
	Total   performancePhaseSummary    `json:"total"`
	Search  *performanceSearchSummary  `json:"search,omitempty"`
	Scan    *performanceScanSummary    `json:"scan,omitempty"`
	Collect *performanceCollectSummary `json:"collect,omitempty"`
	Proxy   *performanceProxySummary   `json:"proxy,omitempty"`
}

// PerformanceEvent is one timed chunk (page fetch, repo scan, scp, etc.).
type PerformanceEvent struct {
	At              time.Time              `json:"at"`
	Phase           string                 `json:"phase"`
	Kind            string                 `json:"kind"`
	Name            string                 `json:"name"`
	DurationSeconds float64                `json:"duration_seconds"`
	OK              bool                   `json:"ok"`
	Detail          map[string]interface{} `json:"detail,omitempty"`
}

type performanceDocument struct {
	Metadata        PerformanceMetadata `json:"metadata"`
	Summary         performanceSummary  `json:"summary"`
	Events          []PerformanceEvent  `json:"events"`
	TruncatedEvents bool                `json:"truncated_events,omitempty"`
}

type perfRecorder struct {
	mu sync.Mutex

	dir   string
	meta  PerformanceMetadata
	start time.Time

	events          []PerformanceEvent
	truncated       bool
	repoDurationSec []float64

	scanTotals      ScanPerformanceMeta
	scanTotalsSet   bool
	searchTotals    SearchPerformanceMeta
	searchTotalsSet bool
}

var activePerf struct {
	sync.Mutex
	rec *perfRecorder
}

func performanceDirForOutput(out string) string {
	if out == "" || out == "-" {
		return "data"
	}
	dir := filepath.Dir(out)
	if dir == "" || dir == "." {
		return "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

func runPerfBegin(dir string, meta PerformanceMetadata) {
	if dir == "" {
		return
	}
	activePerf.Lock()
	defer activePerf.Unlock()
	if activePerf.rec != nil {
		return
	}
	now := time.Now()
	if meta.StartedAt.IsZero() {
		meta.StartedAt = now
	}
	if meta.SchemaVersion == 0 {
		meta.SchemaVersion = performanceSchemaVersion
	}
	if meta.CollectDir == "" {
		meta.CollectDir = dir
	}
	if meta.Hostname == "" {
		meta.Hostname, _ = os.Hostname()
	}
	activePerf.rec = &perfRecorder{
		dir:   dir,
		meta:  meta,
		start: meta.StartedAt,
	}
}

func runPerfActive() *perfRecorder {
	activePerf.Lock()
	defer activePerf.Unlock()
	return activePerf.rec
}

func runPerfEnd() {
	activePerf.Lock()
	activePerf.rec = nil
	activePerf.Unlock()
}

func (r *perfRecorder) record(ev PerformanceEvent) {
	if r == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) >= maxPerformanceEvents {
		r.truncated = true
		return
	}
	r.events = append(r.events, ev)
}

func perfRecordEvent(ev PerformanceEvent) {
	r := runPerfActive()
	if r == nil {
		return
	}
	r.record(ev)
}

func perfRecordSearchPage(source string, page int, repos int, via string, ok bool, d time.Duration) {
	name := fmt.Sprintf("%s page %d", source, page)
	detail := map[string]interface{}{
		"page":  page,
		"repos": repos,
	}
	if via != "" {
		detail["via"] = via
	}
	perfRecordEvent(PerformanceEvent{
		Phase:           "search",
		Kind:            "page",
		Name:            name,
		DurationSeconds: d.Seconds(),
		OK:              ok,
		Detail:          detail,
	})
}

func perfRecordScanRepo(repo string, ok bool, findings, verified int, d time.Duration) {
	r := runPerfActive()
	if r != nil {
		r.mu.Lock()
		if ok {
			r.repoDurationSec = append(r.repoDurationSec, d.Seconds())
		}
		r.mu.Unlock()
	}
	detail := map[string]interface{}{}
	if findings > 0 {
		detail["findings"] = findings
	}
	if verified > 0 {
		detail["verified"] = verified
	}
	perfRecordEvent(PerformanceEvent{
		Phase:           "scan",
		Kind:            "repo",
		Name:            repo,
		DurationSeconds: d.Seconds(),
		OK:              ok,
		Detail:          detail,
	})
}

func perfRecordCollectFile(slave, artifact string, ok bool, d time.Duration) {
	perfRecordEvent(PerformanceEvent{
		Phase:           "collect",
		Kind:            "scp",
		Name:            slave + "/" + artifact,
		DurationSeconds: d.Seconds(),
		OK:              ok,
	})
}

func perfNoteScanComplete(meta ScanPerformanceMeta) {
	r := runPerfActive()
	if r == nil {
		return
	}
	r.mu.Lock()
	r.scanTotals = meta
	r.scanTotalsSet = true
	r.mu.Unlock()
}

func perfNoteSearchComplete(meta SearchPerformanceMeta) {
	r := runPerfActive()
	if r == nil {
		return
	}
	r.mu.Lock()
	r.searchTotals = meta
	r.searchTotalsSet = true
	r.mu.Unlock()
}

func perfRecordPhaseChunk(phase, kind, name string, ok bool, d time.Duration, detail map[string]interface{}) {
	perfRecordEvent(PerformanceEvent{
		Phase:           phase,
		Kind:            kind,
		Name:            name,
		DurationSeconds: d.Seconds(),
		OK:              ok,
		Detail:          detail,
	})
}

func phaseSummaryFrom(d time.Duration, skipped bool) performancePhaseSummary {
	if skipped {
		return performancePhaseSummary{Skipped: true}
	}
	return performancePhaseSummary{
		Duration:        d.Round(time.Millisecond).String(),
		DurationSeconds: d.Seconds(),
	}
}

func proxySummaryFrom(pool *proxy.Pool) *performanceProxySummary {
	if pool == nil {
		return nil
	}
	s := pool.Stats()
	out := &performanceProxySummary{
		Alive:        s.Alive,
		ProxiesAlive: s.ProxiesAlive,
		ReserveAlive: s.ReserveAlive,
		Probed:       s.Probed,
		Kept:         s.Kept,
		Requests:     s.Requests,
		Failures:     s.Failures,
	}
	if s.Probed > 0 {
		out.YieldPercent = 100 * float64(s.Kept) / float64(s.Probed)
	}
	return out
}

func percentileSeconds(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	idx := int(float64(len(cp)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

// RunPerformanceInput aggregates phase wall times and counters for the final write.
type RunPerformanceInput struct {
	CollectDir string
	Playbook   string
	Command    string
	StartedAt  time.Time

	SearchRan  bool
	SearchDur  time.Duration
	SearchMeta *SearchPerformanceMeta

	ScanDur  time.Duration
	ScanMeta *ScanPerformanceMeta

	CollectDur  time.Duration
	CollectMeta *CollectPerformanceMeta

	Pool *proxy.Pool
}

type SearchPerformanceMeta struct {
	Matched     int64
	Calls       int64
	Skipped     int64
	FailedPages int
	Incomplete  bool
}

type ScanPerformanceMeta struct {
	Repos    int
	Failed   int64
	Findings int64
	Verified int64
}

type CollectPerformanceMeta struct {
	FilesCopied int
}

func buildPerformanceDoc(in RunPerformanceInput, rec *perfRecorder) performanceDocument {
	finished := time.Now()
	started := in.StartedAt
	if started.IsZero() && rec != nil {
		started = rec.start
	}
	if started.IsZero() {
		started = finished
	}

	meta := PerformanceMetadata{
		SchemaVersion: performanceSchemaVersion,
		Playbook:      in.Playbook,
		Command:       in.Command,
		CollectDir:    in.CollectDir,
		StartedAt:     started,
		FinishedAt:    finished,
	}
	if meta.Hostname == "" {
		meta.Hostname, _ = os.Hostname()
	}
	if rec != nil {
		if rec.meta.Output != "" {
			meta.Output = rec.meta.Output
		}
		if meta.Command == "" {
			meta.Command = rec.meta.Command
		}
		if meta.Playbook == "" {
			meta.Playbook = rec.meta.Playbook
		}
	}

	totalDur := finished.Sub(started)
	doc := performanceDocument{
		Metadata: meta,
		Summary: performanceSummary{
			Total: phaseSummaryFrom(totalDur, false),
			Proxy: proxySummaryFrom(in.Pool),
		},
	}

	if in.SearchRan || (in.SearchMeta != nil && in.SearchDur > 0) {
		ss := &performanceSearchSummary{
			performancePhaseSummary: phaseSummaryFrom(in.SearchDur, !in.SearchRan),
		}
		sm := in.SearchMeta
		if sm == nil && rec != nil {
			rec.mu.Lock()
			if rec.searchTotalsSet {
				m := rec.searchTotals
				sm = &m
			}
			rec.mu.Unlock()
		}
		if sm != nil {
			ss.Matched = sm.Matched
			ss.Calls = sm.Calls
			ss.SkippedKnown = sm.Skipped
			ss.FailedPages = sm.FailedPages
			ss.Incomplete = sm.Incomplete
		}
		doc.Summary.Search = ss
	} else if in.Command == "playbook" || in.Command == "cluster run" {
		doc.Summary.Search = &performanceSearchSummary{
			performancePhaseSummary: phaseSummaryFrom(0, true),
		}
	}

	if in.ScanDur > 0 || in.ScanMeta != nil {
		sc := &performanceScanSummary{
			performancePhaseSummary: phaseSummaryFrom(in.ScanDur, false),
		}
		sm := in.ScanMeta
		if sm == nil && rec != nil {
			rec.mu.Lock()
			if rec.scanTotalsSet {
				m := rec.scanTotals
				sm = &m
			}
			rec.mu.Unlock()
		}
		if sm != nil {
			sc.Repos = sm.Repos
			sc.Failed = sm.Failed
			sc.Findings = sm.Findings
			sc.Verified = sm.Verified
			if in.ScanDur > 0 && sc.Repos > 0 {
				hours := in.ScanDur.Hours()
				if hours > 0 {
					sc.ReposPerHour = float64(sc.Repos) / hours
				}
			}
		}
		if rec != nil {
			rec.mu.Lock()
			durs := append([]float64(nil), rec.repoDurationSec...)
			rec.mu.Unlock()
			if len(durs) > 0 {
				sc.P50RepoSec = percentileSeconds(durs, 0.50)
				sc.P95RepoSec = percentileSeconds(durs, 0.95)
				sc.MaxRepoSec = durs[0]
				for _, v := range durs[1:] {
					if v > sc.MaxRepoSec {
						sc.MaxRepoSec = v
					}
				}
			}
		}
		doc.Summary.Scan = sc
	}

	if in.CollectDur > 0 || in.CollectMeta != nil {
		co := &performanceCollectSummary{
			performancePhaseSummary: phaseSummaryFrom(in.CollectDur, false),
		}
		if in.CollectMeta != nil {
			co.FilesCopied = in.CollectMeta.FilesCopied
		}
		doc.Summary.Collect = co
	}

	if rec != nil {
		rec.mu.Lock()
		doc.Events = append([]PerformanceEvent(nil), rec.events...)
		doc.TruncatedEvents = rec.truncated
		rec.mu.Unlock()
	}
	return doc
}

func writePerformanceFile(dir string, doc performanceDocument) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, performanceBasename), b, 0644)
}

func finalizeRunPerformance(in RunPerformanceInput) error {
	rec := runPerfActive()
	dir := in.CollectDir
	if dir == "" && rec != nil {
		dir = rec.dir
	}
	doc := buildPerformanceDoc(in, rec)
	if err := writePerformanceFile(dir, doc); err != nil {
		return err
	}
	printPerformanceSummary(dir, doc)
	runPerfEnd()
	return nil
}

func printPerformanceSummary(dir string, doc performanceDocument) {
	fmt.Printf("[+] performance — total %s", doc.Summary.Total.Duration)
	if doc.Summary.Search != nil && !doc.Summary.Search.Skipped {
		fmt.Printf(", search %s", doc.Summary.Search.Duration)
	}
	if doc.Summary.Scan != nil && !doc.Summary.Scan.Skipped {
		fmt.Printf(", scan %s", doc.Summary.Scan.Duration)
		if doc.Summary.Scan.ReposPerHour > 0 {
			fmt.Printf(" (%.1f repos/hr)", doc.Summary.Scan.ReposPerHour)
		}
	}
	if doc.Summary.Collect != nil && !doc.Summary.Collect.Skipped {
		fmt.Printf(", collect %s", doc.Summary.Collect.Duration)
	}
	fmt.Printf(" → %s\n", filepath.Join(dir, performanceBasename))
}

// finalizePlaybookRun writes performance.json for playbook / cluster run completion.
func finalizePlaybookRun(collectDir, name, command string, runStart time.Time, searchRan bool, searchDur, scanDur, collectDur time.Duration, search *SearchPerformanceMeta, scan *ScanPerformanceMeta, collect *CollectPerformanceMeta, pool *proxy.Pool) error {
	if command == "" {
		command = "playbook"
	}
	return finalizeRunPerformance(RunPerformanceInput{
		CollectDir:  collectDir,
		Playbook:    name,
		Command:     command,
		StartedAt:   runStart,
		SearchRan:   searchRan,
		SearchDur:   searchDur,
		SearchMeta:  search,
		ScanDur:     scanDur,
		ScanMeta:    scan,
		CollectDur:  collectDur,
		CollectMeta: collect,
		Pool:        pool,
	})
}

func finalizeSearchPerformance(outPath string, started time.Time, dur time.Duration, meta SearchPerformanceMeta, pool *proxy.Pool) error {
	dir := performanceDirForOutput(outPath)
	return finalizeRunPerformance(RunPerformanceInput{
		CollectDir: dir,
		Command:    "search",
		StartedAt:  started,
		SearchRan:  true,
		SearchDur:  dur,
		SearchMeta: &meta,
		Pool:       pool,
	})
}

func finalizeScanPerformance(reportDir string, started time.Time, dur time.Duration, meta ScanPerformanceMeta, pool *proxy.Pool) error {
	return finalizeRunPerformance(RunPerformanceInput{
		CollectDir: reportDir,
		Command:    "scan",
		StartedAt:  started,
		ScanDur:    dur,
		ScanMeta:   &meta,
		Pool:       pool,
	})
}
