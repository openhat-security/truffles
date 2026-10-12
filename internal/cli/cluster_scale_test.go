package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchPlaybookSlaveWorkers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pb.yaml")
	src := `name: t
slaves:
  - name: worker1
    host: 1.1.1.1
    workers: 2
    enabled: true
  - name: worker3
    host: 2.2.2.2
    workers: 2
    enabled: true
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := patchPlaybookSlaveWorkers(path, []string{"worker3"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("updated = %d", n)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.Contains(got, "name: worker3") || !strings.Contains(got, "workers: 4") {
		t.Fatalf("worker3 not updated:\n%s", got)
	}
	// worker1 unchanged
	if !strings.Contains(got, "name: worker1\n    host: 1.1.1.1\n    workers: 2") {
		t.Fatalf("worker1 changed unexpectedly:\n%s", got)
	}
}

func TestSuggestedWorkersForMachine(t *testing.T) {
	if suggestedWorkersForMachine("e2-standard-2") != 4 {
		t.Fatal("e2-standard-2")
	}
	if suggestedWorkersForMachine("e2-small") != 2 {
		t.Fatal("e2-small")
	}
}

func TestSelectScaleSlaves(t *testing.T) {
	c := &Cluster{Slaves: []Slave{
		{Name: "worker1", Enabled: true, Workers: 6},
		{Name: "worker3", Enabled: true, Workers: 2},
		{Name: "off", Enabled: false, Workers: 2},
	}}
	got, err := selectScaleSlaves(c, "worker3")
	if err != nil || len(got) != 1 || got[0].Name != "worker3" {
		t.Fatalf("got %+v err=%v", got, err)
	}
	all, err := selectScaleSlaves(c, "")
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %+v err=%v", all, err)
	}
}
