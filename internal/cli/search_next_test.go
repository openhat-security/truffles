package cli

import (
	"strings"
	"testing"
)

func TestSearchNextStepsRemoteIncomplete(t *testing.T) {
	hints := searchNextSteps(searchNextOpts{
		outPath:     "data/repos-loop-20261012.txt",
		incomplete:  true,
		failedPages: 4,
		prefixFile:  "examples/prefixes-openai.txt",
		skipFile:    "data/scanned-repos.txt",
		limit:       500,
		hasRemotePB: true,
	})
	if len(hints) < 4 {
		t.Fatalf("expected several hints, got %v", hints)
	}
	if !hints[0].warn || !strings.Contains(hints[0].label, "4 pages") {
		t.Fatalf("expected warn first, got %+v", hints[0])
	}
	foundCluster := false
	for _, h := range hints {
		if strings.Contains(h.cmd, "cluster run") && strings.Contains(h.cmd, "remote-playbook.yaml") {
			foundCluster = true
		}
		if h.label != "" && len(strings.Fields(h.label)) > 6 {
			t.Fatalf("label too long (%q)", h.label)
		}
	}
	if !foundCluster {
		t.Fatalf("missing cluster hint: %v", hints)
	}
}
