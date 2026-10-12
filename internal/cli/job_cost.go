package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Approximate GCP Compute Engine on-demand list prices (us-central1, Linux).
// Estimates only — not a bill. Refresh gcpCostPriceAsOf when updating the table.
const (
	gcpPriceRegion    = "us-central1"
	gcpPDBalancedGBMo = 0.10 // pd-balanced $/GB/month
	gcpHoursPerMonth  = 730.0
	gcpCostPriceAsOf  = "2026-10"
)

type gcpMachinePrice struct {
	Type      string
	VCPUs     int
	MemGB     float64
	HourlyUSD float64
}

var gcpE2OnDemand = []gcpMachinePrice{
	{Type: "e2-small", VCPUs: 2, MemGB: 2, HourlyUSD: 0.0168},
	{Type: "e2-medium", VCPUs: 2, MemGB: 4, HourlyUSD: 0.0335},
	{Type: "e2-standard-2", VCPUs: 2, MemGB: 8, HourlyUSD: 0.0670},
	{Type: "e2-standard-4", VCPUs: 4, MemGB: 16, HourlyUSD: 0.1340},
	{Type: "e2-standard-8", VCPUs: 8, MemGB: 32, HourlyUSD: 0.2680},
	{Type: "e2-standard-16", VCPUs: 16, MemGB: 64, HourlyUSD: 0.5360},
}

type fleetCostEstimate struct {
	Mode          string // "gcp" | "equivalent"
	Region        string
	Lines         []string
	ComputeHourly float64
	DiskHourly    float64
	TotalHourly   float64
	DiskGB        int
	JobSoFarUSD   float64
	JobUptime     time.Duration
}

func pdBalancedHourly(gb int) float64 {
	if gb <= 0 {
		return 0
	}
	return float64(gb) * gcpPDBalancedGBMo / gcpHoursPerMonth
}

func normalizeMachineType(machine string) string {
	m := strings.ToLower(strings.TrimSpace(machine))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	// Strip infer labels like "~8GB/2vCPU".
	if strings.HasPrefix(m, "~") {
		return ""
	}
	return m
}

func lookupMachineHourly(machine string) (float64, bool) {
	m := normalizeMachineType(machine)
	if m == "" {
		return 0, false
	}
	for _, p := range gcpE2OnDemand {
		if p.Type == m {
			return p.HourlyUSD, true
		}
	}
	return 0, false
}

// nearestGCPMachine picks the smallest e2 type that covers probed vCPU + RAM.
func nearestGCPMachine(cpus, memMB int) gcpMachinePrice {
	needGB := float64(memMB) / 1024.0
	if cpus < 1 {
		cpus = 1
	}
	for _, p := range gcpE2OnDemand {
		if p.VCPUs >= cpus && p.MemGB+0.35 >= needGB {
			return p
		}
	}
	return gcpE2OnDemand[len(gcpE2OnDemand)-1]
}

func memTotalMBFromCell(mem string) int {
	parts := strings.Split(mem, "/")
	if len(parts) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return n
}

// estimateFleetCost summarizes on-demand GCP cost for the worker fleet.
// playbookIsGCP is true when any enabled slave has gce_instance set.
func estimateFleetCost(workers []workerStatusRow, jobUptime time.Duration, playbookIsGCP bool) fleetCostEstimate {
	est := fleetCostEstimate{
		Region:    gcpPriceRegion,
		JobUptime: jobUptime,
		Mode:      "equivalent",
	}
	if playbookIsGCP {
		est.Mode = "gcp"
	}

	type key struct {
		label string
		hr    float64
	}
	counts := map[key]int{}
	var compute float64
	var diskGB int

	for _, wr := range workers {
		if !wr.Enabled || wr.SSH == "down" || wr.SSH == "skipped" {
			continue
		}
		memMB := wr.MemTotalMB
		if memMB == 0 {
			memMB = memTotalMBFromCell(wr.Mem)
		}
		// Need a size signal (machine type or probe) — skip empty rows.
		if normalizeMachineType(wr.Machine) == "" && memMB == 0 && wr.CPUs == 0 {
			continue
		}
		if wr.DiskTotalGB > 0 {
			diskGB += wr.DiskTotalGB
		}

		var label string
		var hr float64
		if h, ok := lookupMachineHourly(wr.Machine); ok {
			label = normalizeMachineType(wr.Machine)
			hr = h
		} else {
			eq := nearestGCPMachine(wr.CPUs, memMB)
			hr = eq.HourlyUSD
			if playbookIsGCP {
				label = eq.Type + " (from probe)"
			} else {
				label = eq.Type + " equiv"
			}
		}
		counts[key{label: label, hr: hr}]++
		compute += hr
	}

	est.ComputeHourly = compute
	est.DiskGB = diskGB
	est.DiskHourly = pdBalancedHourly(diskGB)
	est.TotalHourly = est.ComputeHourly + est.DiskHourly
	if jobUptime > 0 {
		est.JobSoFarUSD = est.TotalHourly * jobUptime.Hours()
	}

	type row struct {
		label string
		n     int
		hr    float64
	}
	var rows []row
	for k, n := range counts {
		rows = append(rows, row{k.label, n, k.hr})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].hr != rows[j].hr {
			return rows[i].hr > rows[j].hr
		}
		return rows[i].label < rows[j].label
	})
	for _, r := range rows {
		est.Lines = append(est.Lines, fmt.Sprintf("%d× %s @ $%.4f/hr", r.n, r.label, r.hr))
	}
	return est
}

func formatUSD(v float64) string {
	if v > 0 && v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func printFleetCostEstimate(w io.Writer, p palette, est fleetCostEstimate) {
	if est.TotalHourly <= 0 && len(est.Lines) == 0 {
		return
	}
	fmt.Fprintln(w)
	title := "Cost estimate"
	if est.Mode == "gcp" {
		title = "Cost estimate (GCP fleet)"
	} else {
		title = "Cost estimate (GCP equivalent)"
	}
	p.section(w, title)

	modeLine := fmt.Sprintf("on-demand %s Linux · prices as of %s · not a bill", est.Region, gcpCostPriceAsOf)
	if est.Mode == "equivalent" {
		modeLine = "non-GCP hosts → nearest e2 size from probed CPU/RAM/disk · " + modeLine
	}
	printStatusKVTable(w, p, [][2]string{
		{"Basis", modeLine},
		{"Fleet", strings.Join(est.Lines, "  ·  ")},
		{"Compute", fmt.Sprintf("%s/hr  (~%s/day · ~%s/mo)",
			formatUSD(est.ComputeHourly),
			formatUSD(est.ComputeHourly*24),
			formatUSD(est.ComputeHourly*gcpHoursPerMonth))},
		{"Disk", fmt.Sprintf("%s/hr  (%d GB pd-balanced @ $%.2f/GB·mo)",
			formatUSD(est.DiskHourly), est.DiskGB, gcpPDBalancedGBMo)},
		{"Total", p.bgreen(fmt.Sprintf("%s/hr  (~%s/day · ~%s/mo)",
			formatUSD(est.TotalHourly),
			formatUSD(est.TotalHourly*24),
			formatUSD(est.TotalHourly*gcpHoursPerMonth)))},
		{"This job", fmt.Sprintf("%s so far (%s uptime)",
			formatUSD(est.JobSoFarUSD), formatUptime(est.JobUptime))},
	})
	fmt.Fprintln(w, p.dim("  excludes egress, snapshots, tax; CUDs/spot would be lower"))
}
