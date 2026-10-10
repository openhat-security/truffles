package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const playbookTimingBasename = "timing.json"

type phaseTiming struct {
	Duration        string  `json:"duration"`
	DurationSeconds float64 `json:"duration_seconds"`
	Skipped         bool    `json:"skipped,omitempty"`
}

type playbookRunTiming struct {
	Playbook   string       `json:"playbook,omitempty"`
	CollectDir string       `json:"collect_dir"`
	StartedAt  time.Time    `json:"started_at"`
	Search     *phaseTiming `json:"search,omitempty"`
	Scan       *phaseTiming `json:"scan,omitempty"`
	Total      phaseTiming  `json:"total"`
}

func phaseTimingFrom(d time.Duration, skipped bool) *phaseTiming {
	if skipped {
		return &phaseTiming{Skipped: true}
	}
	return &phaseTiming{
		Duration:        d.Round(time.Millisecond).String(),
		DurationSeconds: d.Seconds(),
	}
}

func buildPlaybookRunTiming(collectDir, name string, runStart time.Time, searchRan bool, searchDur, scanDur time.Duration) playbookRunTiming {
	rec := playbookRunTiming{
		Playbook:   name,
		CollectDir: collectDir,
		StartedAt:  runStart,
		Total: phaseTiming{
			Duration:        time.Since(runStart).Round(time.Millisecond).String(),
			DurationSeconds: time.Since(runStart).Seconds(),
		},
	}
	if searchRan {
		rec.Search = phaseTimingFrom(searchDur, false)
	} else {
		rec.Search = phaseTimingFrom(0, true)
	}
	rec.Scan = phaseTimingFrom(scanDur, false)
	return rec
}

func writePlaybookTiming(collectDir string, rec playbookRunTiming) error {
	if collectDir == "" {
		return nil
	}
	if err := os.MkdirAll(collectDir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(collectDir, playbookTimingBasename), b, 0644)
}

func printPlaybookTimingSummary(rec playbookRunTiming) {
	fmt.Printf("[+] timing — total %s", rec.Total.Duration)
	if rec.Search != nil && !rec.Search.Skipped {
		fmt.Printf(", search %s", rec.Search.Duration)
	}
	if rec.Scan != nil && !rec.Scan.Skipped {
		fmt.Printf(", scan %s", rec.Scan.Duration)
	}
	fmt.Printf(" → %s\n", filepath.Join(rec.CollectDir, playbookTimingBasename))
}

func finalizePlaybookRun(collectDir, name string, runStart time.Time, searchRan bool, searchDur, scanDur time.Duration) error {
	rec := buildPlaybookRunTiming(collectDir, name, runStart, searchRan, searchDur, scanDur)
	if err := writePlaybookTiming(collectDir, rec); err != nil {
		return err
	}
	printPlaybookTimingSummary(rec)
	return nil
}
