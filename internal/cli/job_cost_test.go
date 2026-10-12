package cli

import (
	"strings"
	"testing"
	"time"
)

func TestNearestGCPMachine(t *testing.T) {
	cases := []struct {
		cpus, mem int
		want      string
	}{
		{2, 1900, "e2-small"},
		{2, 1964, "e2-small"},
		{2, 4000, "e2-medium"},
		{2, 7939, "e2-standard-2"},
		{4, 16000, "e2-standard-4"},
		{8, 32000, "e2-standard-8"},
	}
	for _, tc := range cases {
		got := nearestGCPMachine(tc.cpus, tc.mem)
		if got.Type != tc.want {
			t.Fatalf("cpus=%d mem=%d → %s want %s", tc.cpus, tc.mem, got.Type, tc.want)
		}
	}
}

func TestLookupMachineHourly(t *testing.T) {
	h, ok := lookupMachineHourly("e2-standard-2")
	if !ok || h != 0.0670 {
		t.Fatalf("got %v %v", h, ok)
	}
	h, ok = lookupMachineHourly("projects/x/zones/z/machineTypes/e2-small")
	if !ok || h != 0.0168 {
		t.Fatalf("path basename: %v %v", h, ok)
	}
	if _, ok := lookupMachineHourly("~8GB/2vCPU"); ok {
		t.Fatal("infer label should not lookup")
	}
}

func TestEstimateFleetCostGCP(t *testing.T) {
	workers := []workerStatusRow{
		{Name: "w1", Enabled: true, Machine: "e2-standard-2", CPUs: 2, MemTotalMB: 7939, DiskTotalGB: 50},
		{Name: "w2", Enabled: true, Machine: "e2-standard-2", CPUs: 2, MemTotalMB: 7939, DiskTotalGB: 50},
		{Name: "w3", Enabled: true, Machine: "e2-small", CPUs: 2, MemTotalMB: 1964, DiskTotalGB: 50},
		{Name: "w4", Enabled: true, Machine: "e2-small", CPUs: 2, MemTotalMB: 1964, DiskTotalGB: 50},
		{Name: "w5", Enabled: true, Machine: "e2-small", CPUs: 2, MemTotalMB: 1964, DiskTotalGB: 50},
		{Name: "w6", Enabled: true, Machine: "e2-small", CPUs: 2, MemTotalMB: 1964, DiskTotalGB: 50},
		{Name: "w7", Enabled: true, Machine: "e2-small", CPUs: 2, MemTotalMB: 1964, DiskTotalGB: 50},
	}
	est := estimateFleetCost(workers, 2*time.Hour, true)
	if est.Mode != "gcp" {
		t.Fatalf("mode = %s", est.Mode)
	}
	// 2*0.067 + 5*0.0168 = 0.134 + 0.084 = 0.218
	wantCompute := 2*0.0670 + 5*0.0168
	if est.ComputeHourly < wantCompute-0.0001 || est.ComputeHourly > wantCompute+0.0001 {
		t.Fatalf("compute = %f want %f", est.ComputeHourly, wantCompute)
	}
	if est.DiskGB != 350 {
		t.Fatalf("disk = %d", est.DiskGB)
	}
	if est.JobSoFarUSD <= 0 {
		t.Fatal("expected job so far > 0")
	}
	joined := strings.Join(est.Lines, " ")
	if !strings.Contains(joined, "e2-standard-2") || !strings.Contains(joined, "e2-small") {
		t.Fatalf("lines = %v", est.Lines)
	}
}

func TestEstimateFleetCostEquivalent(t *testing.T) {
	workers := []workerStatusRow{
		{Name: "local", Enabled: true, CPUs: 2, Mem: "500/7939", MemTotalMB: 7939, DiskTotalGB: 100},
	}
	est := estimateFleetCost(workers, time.Hour, false)
	if est.Mode != "equivalent" {
		t.Fatalf("mode = %s", est.Mode)
	}
	if !strings.Contains(est.Lines[0], "e2-standard-2") {
		t.Fatalf("lines = %v", est.Lines)
	}
	if est.ComputeHourly != 0.0670 {
		t.Fatalf("hourly = %f", est.ComputeHourly)
	}
}

func TestParseWorkerProbeOutputDiskGB(t *testing.T) {
	var row workerStatusRow
	parseWorkerProbeOutput("120|0.5 0.4 0.3|2|512/7841|34|49|12", &row, true)
	if row.Disk != "34%" || row.DiskTotalGB != 49 || row.CPUPct != "12%" || row.MemTotalMB != 7841 {
		t.Fatalf("got %+v", row)
	}
}
