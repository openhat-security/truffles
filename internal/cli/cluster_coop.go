package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runClusterCoop(rest []string) error {
	fs := flag.NewFlagSet("cluster coop", flag.ContinueOnError)
	f := fs.String("f", "repos.txt", "repos list")
	queue := fs.String("queue", "workqueue.json", "shared work queue path (on master/shared FS)")
	job := fs.String("job", "", "job id for this node")
	out := fs.String("out", "", "output report base name")
	format := fs.String("format", "csv", "format")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *job == "" {
		*job = fmt.Sprintf("node-%d", os.Getpid())
	}
	if *out == "" {
		*out = fmt.Sprintf("results-%s", *job)
	}

	// Try to load queue; if not exists, create from file
	wq, err := LoadWorkQueue(*queue)
	if err != nil {
		urls := readURLsList(*f)
		wq, err = NewWorkQueue(*queue, urls)
		if err != nil {
			return err
		}
	}

	// Work loop: claim and process items (simple sequential for now)
	done := 0
	for {
		url, ok := wq.Claim(*job)
		if !ok {
			break // no work left
		}
		// Write temp single-url file
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("coop-%s.txt", strings.ReplaceAll(*job, "/", "-")))
		writeLines(filepath.Dir(tmp), filepath.Base(tmp), []string{url})
		// Run scan on this single repo
		// Use runScan logic indirectly? simpler: call trufflehog directly via scan logic
		// but we want to integrate; just simulate by creating a temp scan
		// easier: write single file and run a quick scan
		report := fmt.Sprintf("%s-%d.csv", *out, done)
		if *format == "jsonl" {
			report = fmt.Sprintf("%s-%d.jsonl", *out, done)
		}
		// Run scan on single file
		sargs := []string{"-f", tmp, "-format", *format, "-out", report, "-workers", "1"}
		if err := runScan(sargs); err != nil {
			wq.Fail(url, *job)
		} else {
			wq.Done(url, *job)
		}
		done++
		os.Remove(tmp)
	}
	fmt.Printf("coop job %s done (%d items)\n", *job, done)
	return nil
}
