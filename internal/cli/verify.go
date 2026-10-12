package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// runVerify re-checks secrets from a truffles results CSV via `trufflehog stdin`
// (live verification, no git clone) and writes only verified rows to
// <input-stem>-verified.csv. file:// repository_url values are repaired from
// sibling pretty reports (results-*.txt) in the same directory when possible.
func runVerify(args []string) error {
	fs := newFlagSet("verify")
	filePath := fs.String("f", "", "Results CSV to verify")
	fs.StringVar(filePath, "file", "", "Results CSV to verify")
	outPath := fs.String("out", "", "Output CSV (default: <input>-verified.csv)")
	bin := fs.String("bin", "trufflehog", "trufflehog binary")
	workers := fs.Int("w", 4, "Concurrent verifications")
	fs.IntVar(workers, "workers", 4, "Concurrent verifications")
	detectors := fs.String("detectors", "", "Only these detector names (comma-separated; empty = all)")
	limit := fs.Int("limit", 0, "Max unique secrets to verify (0 = all)")
	progressEvery := fs.Duration("progress", 10*time.Second, "Progress heartbeat; 0 disables")
	if err := parseFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(*filePath) == "" {
		return fmt.Errorf("-f <results.csv> required")
	}
	if _, err := exec.LookPath(*bin); err != nil {
		return fmt.Errorf("trufflehog binary %q not usable (override with -bin): %w", *bin, err)
	}
	if *workers < 1 {
		*workers = 1
	}

	inAbs, err := filepath.Abs(*filePath)
	if err != nil {
		return err
	}
	out := *outPath
	if out == "" {
		out = verifiedCSVPath(inAbs)
	}

	rows, header, err := readResultsCSV(inAbs)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no data rows in %s", inAbs)
	}

	secretRepo := loadSecretRepoMap(filepath.Dir(inAbs))
	repaired := 0
	for i := range rows {
		u := strings.TrimSpace(rows[i]["repository_url"])
		if githubRepoURL(u) != "" {
			rows[i]["repository_url"] = githubRepoURL(u)
			continue
		}
		if sec := strings.TrimSpace(rows[i]["secret"]); sec != "" {
			if gh, ok := secretRepo[sec]; ok {
				rows[i]["repository_url"] = gh
				repaired++
			}
		}
	}
	if repaired > 0 {
		fmt.Fprintf(os.Stderr, "[*] repaired %d repository_url(s) from sibling results-*.txt\n", repaired)
	}

	allowDet := parseDetectorSet(*detectors)
	type job struct {
		secret string
		rows   []map[string]string
	}
	bySecret := map[string]*job{}
	var order []string
	for _, row := range rows {
		sec := strings.TrimSpace(row["secret"])
		if sec == "" {
			continue
		}
		if len(allowDet) > 0 {
			if _, ok := allowDet[strings.ToLower(strings.TrimSpace(row["detector"]))]; !ok {
				continue
			}
		}
		if j, ok := bySecret[sec]; ok {
			j.rows = append(j.rows, row)
			continue
		}
		bySecret[sec] = &job{secret: sec, rows: []map[string]string{row}}
		order = append(order, sec)
	}
	if *limit > 0 && len(order) > *limit {
		order = order[:*limit]
	}
	if len(order) == 0 {
		return fmt.Errorf("nothing to verify (rows=%d)", len(rows))
	}

	fmt.Fprintf(os.Stderr, "[*] verifying %d unique secret(s) from %s (%d workers) → %s\n",
		len(order), inAbs, *workers, out)

	type result struct {
		secret   string
		verified bool
		detName  string
		verr     string
	}
	jobs := make(chan string, *workers*2)
	results := make(chan result, *workers*2)
	var wg sync.WaitGroup
	ctx := context.Background()
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for secret := range jobs {
				ok, det, verr := verifySecret(ctx, *bin, secret)
				results <- result{secret: secret, verified: ok, detName: det, verr: verr}
			}
		}()
	}
	go func() {
		for _, sec := range order {
			jobs <- sec
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var (
		done     atomic.Int64
		verified atomic.Int64
		total    = int64(len(order))
	)
	stopHB := make(chan struct{})
	if *progressEvery > 0 {
		go func() {
			t := time.NewTicker(*progressEvery)
			defer t.Stop()
			for {
				select {
				case <-stopHB:
					return
				case <-t.C:
					d, v := done.Load(), verified.Load()
					fmt.Fprintf(os.Stderr, "[*] %d/%d checked, %d verified so far\n", d, total, v)
				}
			}
		}()
	}

	verifiedSecrets := map[string]result{}
	for r := range results {
		done.Add(1)
		if r.verified {
			verified.Add(1)
			verifiedSecrets[r.secret] = r
			fmt.Fprintf(os.Stderr, "[+] verified %s (%s)\n", shortSecret(r.secret), r.detName)
		}
	}
	close(stopHB)

	var outRows []map[string]string
	for _, sec := range order {
		vr, ok := verifiedSecrets[sec]
		if !ok {
			continue
		}
		row := bySecret[sec].rows[0]
		outRow := copyRow(row, header)
		outRow["verified"] = "true"
		outRow["verification_error"] = vr.verr
		if u := githubRepoURL(outRow["repository_url"]); u != "" {
			outRow["repository_url"] = u
		} else if gh, ok := secretRepo[sec]; ok {
			outRow["repository_url"] = gh
		}
		if d := strings.TrimSpace(vr.detName); d != "" && strings.TrimSpace(outRow["detector"]) == "" {
			outRow["detector"] = d
		}
		outRows = append(outRows, outRow)
	}

	if err := writeResultsCSV(out, header, outRows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[+] done — %d/%d verified, wrote %s\n", len(outRows), len(order), out)
	return nil
}

func verifiedCSVPath(in string) string {
	dir := filepath.Dir(in)
	base := filepath.Base(in)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".csv"
	}
	return filepath.Join(dir, stem+"-verified"+ext)
}

func parseDetectorSet(s string) map[string]struct{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := map[string]struct{}{}
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out[p] = struct{}{}
		}
	}
	return out
}

func copyRow(row map[string]string, header []string) map[string]string {
	out := make(map[string]string, len(header))
	for _, h := range header {
		out[h] = row[h]
	}
	return out
}

func shortSecret(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 16 {
		return s
	}
	return s[:10] + "…" + s[len(s)-4:]
}

func githubRepoURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" || strings.HasPrefix(u, "file:") {
		return ""
	}
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	if strings.HasPrefix(u, "https://github.com/") || strings.HasPrefix(u, "http://github.com/") {
		return strings.Replace(u, "http://github.com/", "https://github.com/", 1)
	}
	// owner/name short form from pretty reports
	if strings.Count(u, "/") == 1 && !strings.Contains(u, "://") {
		return "https://github.com/" + u
	}
	return ""
}

func readResultsCSV(path string) ([]map[string]string, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.ReuseRecord = false
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, nil, err
	}
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}
	var rows []map[string]string
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		row := make(map[string]string, len(header))
		for i, h := range header {
			if i < len(rec) {
				row[h] = rec[i]
			} else {
				row[h] = ""
			}
		}
		rows = append(rows, row)
	}
	return rows, header, nil
}

func writeResultsCSV(path string, header []string, rows []map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		rec := make([]string, len(header))
		for i, h := range header {
			rec[i] = row[h]
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

var (
	rePrettyHit    = regexp.MustCompile(`^\[\+\+\]\s+(\S+/\S+)`)
	rePrettySecret = regexp.MustCompile(`^\s+secret\s+(.+)\s*$`)
)

// loadSecretRepoMap maps raw secret → https://github.com/owner/repo using
// sibling pretty reports in dir (results-*.txt / *.txt).
func loadSecretRepoMap(dir string) map[string]string {
	out := map[string]string{}
	patterns := []string{
		filepath.Join(dir, "results-*.txt"),
		filepath.Join(dir, "*.txt"),
	}
	seenFile := map[string]struct{}{}
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		for _, path := range matches {
			if _, ok := seenFile[path]; ok {
				continue
			}
			seenFile[path] = struct{}{}
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
			current := ""
			for sc.Scan() {
				line := sc.Text()
				if m := rePrettyHit.FindStringSubmatch(line); m != nil {
					current = "https://github.com/" + strings.TrimSuffix(m[1], "/")
					continue
				}
				if current == "" {
					continue
				}
				if m := rePrettySecret.FindStringSubmatch(line); m != nil {
					sec := strings.TrimSpace(m[1])
					if sec != "" {
						if _, exists := out[sec]; !exists {
							out[sec] = current
						}
					}
				}
			}
			f.Close()
		}
	}
	return out
}

// verifySecret runs trufflehog stdin on one secret. Returns whether any finding
// was Verified, plus detector name / verification error from the best hit.
func verifySecret(ctx context.Context, bin, secret string) (bool, string, string) {
	cmd := exec.CommandContext(ctx, bin, "stdin", "--no-update", "--json",
		"--results=verified,unverified,unknown")
	cmd.Stdin = strings.NewReader(secret + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	type thFinding struct {
		Verified          bool   `json:"Verified"`
		DetectorName      string `json:"DetectorName"`
		VerificationError string `json:"VerificationError"`
		Raw               string `json:"Raw"`
		RawV2             string `json:"RawV2"`
	}

	bestDet, bestErr := "", ""
	verified := false
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var f thFinding
		if err := json.Unmarshal(line, &f); err != nil {
			continue
		}
		raw := f.RawV2
		if raw == "" {
			raw = f.Raw
		}
		// Accept detector hit if raw matches or is empty (some detectors omit RawV2 shape).
		if raw != "" && raw != secret && !strings.Contains(secret, raw) && !strings.Contains(raw, secret) {
			continue
		}
		if f.DetectorName != "" {
			bestDet = f.DetectorName
		}
		if f.VerificationError != "" {
			bestErr = f.VerificationError
		}
		if f.Verified {
			verified = true
			bestErr = f.VerificationError
			break
		}
	}
	return verified, bestDet, bestErr
}
