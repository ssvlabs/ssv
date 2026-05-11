package reporting

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// RenderBatchCSV writes a BatchRun as CSV with one row per (scenario,
// protocol) cell. Columns capture the distribution summary so the CSV
// is self-contained for spreadsheet analysis:
//
//	Scenario, Protocol, Iterations, SuccessRate,
//	DecisionTime_P50_ms, DecisionTime_P90_ms, DecisionTime_P99_ms,
//	DecisionTime_Mean_ms, DecisionTime_Max_ms,
//	Bandwidth_P50_B, Bandwidth_P99_B, Bandwidth_Mean_B,
//	Evidence_Total, MissReason_Top
//
// Cells with Iterations=0 (not applicable) emit a row with empty
// distribution columns so spreadsheet pivots see the n/a explicitly.
func RenderBatchCSV(br *BatchRun, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("reporting: create batch CSV %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"Scenario", "Protocol", "Iterations", "SuccessRate",
		"DecisionTime_P50_ms", "DecisionTime_P90_ms", "DecisionTime_P99_ms",
		"DecisionTime_Mean_ms", "DecisionTime_Max_ms",
		"Bandwidth_P50_B", "Bandwidth_P99_B", "Bandwidth_Mean_B",
		"Evidence_Total", "MissReason_Top",
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("reporting: write batch CSV header: %w", err)
	}

	for _, scenario := range br.Scenarios {
		for _, protocol := range br.ProtocolNames {
			cell, ok := br.CellAt(scenario, protocol)
			if !ok {
				continue
			}
			row := []string{
				scenario, protocol,
				strconv.Itoa(cell.Iterations),
				strconv.FormatFloat(cell.SuccessRate, 'f', 4, 64),
			}
			if Applicable(cell) {
				row = append(row,
					formatNum(cell.DecisionTime.Percentile(50)),
					formatNum(cell.DecisionTime.Percentile(90)),
					formatNum(cell.DecisionTime.Percentile(99)),
					formatNum(cell.DecisionTime.Mean()),
					formatNum(cell.DecisionTime.Max()),
					formatNum(cell.ClusterBandwidth.Percentile(50)),
					formatNum(cell.ClusterBandwidth.Percentile(99)),
					formatNum(cell.ClusterBandwidth.Mean()),
					strconv.Itoa(totalBatchEvidence(cell)),
					topMissReason(cell),
				)
			} else {
				row = append(row, "", "", "", "", "", "", "", "", "0", "")
			}
			if err := w.Write(row); err != nil {
				return fmt.Errorf("reporting: write batch CSV row: %w", err)
			}
		}
	}
	return nil
}

func formatNum(f float64) string {
	return strconv.FormatFloat(f, 'f', 0, 64)
}

// totalBatchEvidence sums Mean evidence count across all rule keys in
// the cell. Approximates "average fires per sim across all rules".
func totalBatchEvidence(c ct.BatchCell) int {
	total := 0.0
	for _, dist := range c.EvidenceCounts {
		total += dist.Mean()
	}
	return int(total + 0.5)
}

// topMissReason returns the most common miss-reason label for the cell,
// or empty string if no misses (100% success). For cells with multiple
// reasons, returns the one with the highest count.
func topMissReason(c ct.BatchCell) string {
	if len(c.MissReasons) == 0 {
		return ""
	}
	top, topCount := "", 0
	for k, n := range c.MissReasons {
		if n > topCount {
			top = k
			topCount = n
		}
	}
	return top
}
