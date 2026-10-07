package cli

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Finding is one secret reported by trufflehog. trufflehog emits one JSON
// object per line, which is unreadable as-is — this is the subset worth
// showing, flattened out of the nested SourceMetadata envelope.
type Finding struct {
	Verified        bool   `json:"Verified"`
	VerificationErr string `json:"VerificationError"`
	DetectorName    string `json:"DetectorName"`
	DetectorDesc    string `json:"DetectorDescription"`
	DecoderName     string `json:"DecoderName"`
	Raw             string `json:"Raw"`
	RawV2           string `json:"RawV2"`
	Redacted        string `json:"Redacted"`

	SourceMetadata struct {
		Data struct {
			Git struct {
				Repository string `json:"repository"`
				File       string `json:"file"`
				Line       int    `json:"line"`
				Commit     string `json:"commit"`
				Email      string `json:"email"`
				Timestamp  string `json:"timestamp"`
			} `json:"Git"`
		} `json:"Data"`
	} `json:"SourceMetadata"`
}

// Repo is the owning repo, if the finding came from git history.
func (f Finding) Repo() string { return f.SourceMetadata.Data.Git.Repository }

// Location renders file:line, or just file.
func (f Finding) Location() string {
	g := f.SourceMetadata.Data.Git
	if g.File == "" {
		return ""
	}
	if g.Line > 0 {
		return fmt.Sprintf("%s:%d", g.File, g.Line)
	}
	return g.File
}

// CommitLine renders the short commit, author and date.
func (f Finding) CommitLine() string {
	g := f.SourceMetadata.Data.Git
	if g.Commit == "" {
		return ""
	}
	short := g.Commit
	if len(short) > 8 {
		short = short[:8]
	}
	out := short
	if g.Timestamp != "" {
		if t, err := time.Parse("2006-01-02 15:04:05 -0700", g.Timestamp); err == nil {
			out += "  " + t.Format("2006-01-02")
		} else {
			out += "  " + g.Timestamp
		}
	}
	if g.Email != "" {
		out += "  " + g.Email
	}
	return out
}

// Secret returns the plaintext secret from trufflehog.
// Never use Redacted — that field is masked (e.g. user:********@host).
// If Raw/RawV2 themselves contain asterisks, that text came from the scanned
// source (or trufflehog wrote stars into Raw); we do not mask on our side.
func (f Finding) Secret() string {
	if f.Raw != "" {
		return f.Raw
	}
	return f.RawV2
}

// defaultReportName derives "<inputstem>-<unixtimestamp>.<ext>" so successive
// runs never overwrite each other. The extension follows -format.
func defaultReportName(input, format string) string {
	ext := ".txt"
	switch format {
	case "csv":
		ext = ".csv"
	case "jsonl":
		ext = ".jsonl"
	}
	base := filepath.Base(input)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		stem = "truffles"
	}
	return fmt.Sprintf("%s-%d%s", stem, time.Now().Unix(), ext)
}

// dualReportPaths returns the on-disk pretty and csv paths for a primary -out.
// jsonl stays single-file; pretty and csv always get a sibling of the other.
func dualReportPaths(out, format string) (prettyPath, csvPath, jsonlPath string) {
	stem := strings.TrimSuffix(out, filepath.Ext(out))
	if stem == "" {
		stem = out
	}
	switch format {
	case "csv":
		return stem + ".txt", out, ""
	case "jsonl":
		return "", "", out
	default: // pretty
		return out, stem + ".csv", ""
	}
}

// reportSink writes findings to pretty and/or csv (and optionally jsonl).
// When writing to disk with -format pretty or csv, both pretty and csv files
// are opened so local collects always get a human report alongside CSV.
type reportSink struct {
	mu sync.Mutex

	prettyPath string
	csvPath    string
	jsonlPath  string

	prettyFile *os.File
	csvFile    *os.File
	jsonlFile  *os.File

	pretty *bufio.Writer
	csv    *bufio.Writer
	jsonl  *bufio.Writer

	rp       palette
	toStdout bool
	format   string
}

func openReportSink(out, format, colorMode string, toStdout bool) (*reportSink, error) {
	s := &reportSink{toStdout: toStdout, format: format}
	if toStdout {
		dest := os.Stdout
		s.rp = palette{resolveColor(colorMode, dest)}
		bw := bufio.NewWriterSize(dest, 32*1024)
		switch format {
		case "csv":
			s.csv = bw
			s.csvFile = dest
			writeCSVHeader(s.csv)
			_ = s.csv.Flush()
		case "jsonl":
			s.jsonl = bw
			s.jsonlFile = dest
		default:
			s.pretty = bw
			s.prettyFile = dest
		}
		return s, nil
	}

	prettyPath, csvPath, jsonlPath := dualReportPaths(out, format)
	s.prettyPath, s.csvPath, s.jsonlPath = prettyPath, csvPath, jsonlPath

	open := func(path string) (*os.File, *bufio.Writer, error) {
		f, err := os.Create(path)
		if err != nil {
			return nil, nil, err
		}
		return f, bufio.NewWriterSize(f, 32*1024), nil
	}

	if prettyPath != "" {
		f, bw, err := open(prettyPath)
		if err != nil {
			return nil, err
		}
		s.prettyFile, s.pretty = f, bw
		s.rp = palette{resolveColor(colorMode, f)}
	}
	if csvPath != "" {
		f, bw, err := open(csvPath)
		if err != nil {
			s.Close()
			return nil, err
		}
		s.csvFile, s.csv = f, bw
		writeCSVHeader(s.csv)
		_ = s.csv.Flush()
		_ = s.csvFile.Sync()
		if s.prettyFile == nil {
			s.rp = palette{resolveColor(colorMode, f)}
		}
	}
	if jsonlPath != "" {
		f, bw, err := open(jsonlPath)
		if err != nil {
			s.Close()
			return nil, err
		}
		s.jsonlFile, s.jsonl = f, bw
		if s.prettyFile == nil && s.csvFile == nil {
			s.rp = palette{resolveColor(colorMode, f)}
		}
	}
	return s, nil
}

func (s *reportSink) paths() []string {
	var out []string
	for _, p := range []string{s.prettyPath, s.csvPath, s.jsonlPath} {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *reportSink) flush() {
	if s.pretty != nil {
		_ = s.pretty.Flush()
	}
	if s.csv != nil {
		_ = s.csv.Flush()
	}
	if s.jsonl != nil {
		_ = s.jsonl.Flush()
	}
	for _, f := range []*os.File{s.prettyFile, s.csvFile, s.jsonlFile} {
		if f != nil && f != os.Stdout {
			_ = f.Sync()
		}
	}
}

func (s *reportSink) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flush()
	closeOne := func(f **os.File) {
		if *f != nil && *f != os.Stdout {
			_ = (*f).Close()
		}
		*f = nil
	}
	closeOne(&s.prettyFile)
	closeOne(&s.csvFile)
	closeOne(&s.jsonlFile)
	s.pretty, s.csv, s.jsonl = nil, nil, nil
}

func (s *reportSink) writeRepo(repo string, res Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pretty != nil {
		renderRepo(s.pretty, s.rp, repo, res)
	}
	if s.csv != nil {
		writeCSV(s.csv, res)
	}
	if s.jsonl != nil {
		writeJSONL(s.jsonl, res)
	}
	s.flush()
}

func (s *reportSink) writeSummary(repos int, fail, findings, verified int64, elapsed time.Duration, byDetector *sync.Map) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pretty == nil {
		return
	}
	rp := s.rp
	fmt.Fprintf(s.pretty, "\n%s\n", rp.bold("=== Summary ==="))
	fmt.Fprintf(s.pretty, "  %s %d\n", rp.dim(fmt.Sprintf("%-22s", "Repos scanned")), repos)
	fmt.Fprintf(s.pretty, "  %s %d\n", rp.dim(fmt.Sprintf("%-22s", "Failed")), fail)
	fmt.Fprintf(s.pretty, "  %s %d\n", rp.dim(fmt.Sprintf("%-22s", "Findings")), findings)
	verdict := rp.yellow(fmt.Sprintf("%d unverified", findings-verified))
	if verified > 0 {
		verdict = rp.bgreen(fmt.Sprintf("%d verified", verified)) + ", " + verdict
	}
	fmt.Fprintf(s.pretty, "  %s %s\n", rp.dim(fmt.Sprintf("%-22s", "Verification")), verdict)
	fmt.Fprintf(s.pretty, "  %s %s\n", rp.dim(fmt.Sprintf("%-22s", "Elapsed")), elapsed)

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
		fmt.Fprintf(s.pretty, "\n  %s\n", rp.bold("By detector"))
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].n != rows[j].n {
				return rows[i].n > rows[j].n
			}
			return rows[i].name < rows[j].name
		})
		for _, r := range rows {
			fmt.Fprintf(s.pretty, "    %s %s\n", pad(r.name, 30), rp.cyan(fmt.Sprintf("%d", r.n)))
		}
	}
	s.flush()
}

type Result struct {
	Repo     string
	Output   string
	Err      error
	Duration time.Duration
	Findings []Finding
	Lines    int
	Logs     string
}

// writeCSV appends one row per finding. Only repos with findings appear;
// consult the pretty report or the summary for clean/failed counts.
// csvColumns is the CSV column order. It is written once per report, before
// any repo completes, so the header is on disk immediately.
var csvColumns = []string{"detector", "verified", "timestamp", "secret", "repository_url", "file_line",
	"commit", "author", "decoder", "verification_error"}

// writeCSVHeader emits the column names and flushes.
func writeCSVHeader(w io.Writer) {
	cw := csv.NewWriter(w)
	_ = cw.Write(csvColumns)
	cw.Flush()
}

// writeCSV appends one row per finding. The header is not repeated per repo.
func writeCSV(w io.Writer, res Result) {
	cw := csv.NewWriter(w)
	for _, f := range res.Findings {
		repo := f.Repo()
		if repo == "" {
			repo = res.Repo
		}
		g := f.SourceMetadata.Data.Git
		fl := g.File
		if g.Line > 0 {
			fl = g.File + ":" + strconv.Itoa(g.Line)
		}
		cw.Write([]string{
			f.DetectorName,
			strconv.FormatBool(f.Verified),
			g.Timestamp,
			f.Secret(),
			repo,
			fl,
			g.Commit,
			g.Email,
			f.DecoderName,
			f.VerificationErr,
		})
	}
	cw.Flush()
}

// writeJSONL appends one compact JSON object per finding. JSON Lines (rather
// than a single array) so the file stays valid and complete after every repo,
// matching trufflehog's own one-object-per-line convention.
func writeJSONL(w io.Writer, res Result) {
	enc := json.NewEncoder(w)
	for _, f := range res.Findings {
		if f.Repo() == "" {
			f.SourceMetadata.Data.Git.Repository = res.Repo
		}
		enc.Encode(f)
	}
}

// prettyWriter adapts a strings.Builder for statusf.
type prettyWriter struct{ sb *strings.Builder }

func (w prettyWriter) Write(b []byte) (int, error) { return w.sb.Write(b) }

// renderRepo writes one repo's section of the pretty report.
func renderRepo(w io.Writer, p palette, repo string, res Result) {
	name := shortRepo(repo)
	if res.Err != nil {
		fmt.Fprintf(w, "\n%s %s %s\n", p.bred("[!!]"), p.bold(name),
			p.dim(fmt.Sprintf("(%s)", res.Duration.Round(time.Millisecond))))
		fmt.Fprintf(w, "      %s %v\n", p.dim("error:"), res.Err)
		return
	}
	if len(res.Findings) == 0 {
		if res.Lines == 0 {
			fmt.Fprintf(w, "\n%s %s %s\n", p.bgreen("[ok]"), p.bold(name),
				p.dim(fmt.Sprintf("(%s) clean", res.Duration.Round(time.Millisecond))))
			return
		}

		fmt.Fprintf(w, "\n%s %s\n", p.bcyan("[--]"), p.bold(name))
		fmt.Fprint(w, res.Output)
		if !strings.HasSuffix(res.Output, "\n") {
			fmt.Fprintln(w)
		}
		return
	}

	nv := 0
	for _, f := range res.Findings {
		if !f.Verified {
			nv++
		}
	}
	head := fmt.Sprintf("\n%s %s", p.byellow("[++]"), p.bold(name))
	head += p.dim(fmt.Sprintf("  %d finding(s) in %s", len(res.Findings), res.Duration.Round(time.Millisecond)))
	if nv == len(res.Findings) {
		head += "  " + p.byellow("all unverified")
	}
	fmt.Fprintln(w, head)

	for _, f := range res.Findings {
		badge := p.yellow("UNVERIFIED")
		if f.Verified {
			badge = p.bgreen("VERIFIED  ")
		}
		label := f.DetectorName
		if label == "" {
			label = "unknown detector"
		}
		desc := ""
		if f.DetectorDesc != "" {
			desc = p.dim("  " + f.DetectorDesc)
		}
		fmt.Fprintf(w, "\n  %s %s%s\n", badge, p.bcyan(label), desc)

		fmt.Fprintf(w, "      %s %s\n", p.dim(pad("secret", 10)), p.red(f.Secret()))
		if loc := f.Location(); loc != "" {
			fmt.Fprintf(w, "      %s %s\n", p.dim(pad("file", 10)), loc)
		}
		if c := f.CommitLine(); c != "" {
			fmt.Fprintf(w, "      %s %s\n", p.dim(pad("commit", 10)), p.dim(c))
		}
		if f.DecoderName != "" {
			fmt.Fprintf(w, "      %s %s\n", p.dim(pad("decoder", 10)), f.DecoderName)
		}
		if !f.Verified && f.VerificationErr != "" {
			fmt.Fprintf(w, "      %s %s\n", p.dim(pad("why", 10)), p.dim(f.VerificationErr))
		}
	}
}

// shortRepo trims the URL down to owner/name for the status line.
func shortRepo(repo string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(repo, "https://github.com/"), "http://github.com/")
	return strings.TrimSuffix(s, "/")
}

// parseFindings decodes trufflehog's one-JSON-object-per-line output.
// Unparseable lines are counted rather than discarded.
func parseFindings(out string, jsonOut bool) ([]Finding, int) {
	var fs []Finding
	lines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines++
		if !jsonOut {
			continue
		}
		var f Finding
		if err := json.Unmarshal([]byte(line), &f); err == nil && f.DetectorName != "" {
			fs = append(fs, f)
		}
	}
	return fs, lines
}
