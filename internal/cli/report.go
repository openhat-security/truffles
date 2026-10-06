package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
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

// Secret picks the best available secret text.
func (f Finding) Secret() string {
	if f.Redacted != "" {
		return f.Redacted
	}
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
var csvColumns = []string{"repo", "verified", "detector", "decoder", "file", "line",
	"commit", "timestamp", "author", "secret", "verification_error"}

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
		cw.Write([]string{
			repo,
			strconv.FormatBool(f.Verified),
			f.DetectorName,
			f.DecoderName,
			g.File,
			strconv.Itoa(g.Line),
			g.Commit,
			g.Timestamp,
			g.Email,
			f.Secret(),
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
