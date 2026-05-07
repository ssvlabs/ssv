package reporting

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// RenderCSV writes the run as CSV with one row per (scenario, protocol) cell.
// Columns: Scenario, Protocol, Outcome, DecisionTime, DecidedRound,
// TotalBandwidth, EvidenceTotal.
func RenderCSV(r *Run, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("reporting: create CSV %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{"Scenario", "Protocol", "Outcome", "DecisionTime_ms", "DecidedRound", "TotalBytes", "EvidenceTotal"}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("reporting: write CSV header: %w", err)
	}

	for _, scenario := range r.Scenarios {
		for _, protocol := range r.ProtocolNames {
			cell, ok := r.Cells[CellKey{Scenario: scenario, Protocol: protocol}]
			if !ok {
				continue
			}
			row := []string{
				scenario,
				protocol,
				cellSummary(cell),
				strconv.FormatInt(cell.Outcome.DecisionTime.Milliseconds(), 10),
				strconv.Itoa(cell.Outcome.DecidedRound),
				strconv.FormatInt(cell.Outcome.Bandwidth.TotalBytes, 10),
				strconv.Itoa(totalEvidence(cell)),
			}
			if err := w.Write(row); err != nil {
				return fmt.Errorf("reporting: write CSV row: %w", err)
			}
		}
	}
	return nil
}

// totalEvidence returns the sum of EvidenceByRule fires across all
// operators in the cell's outcome.
func totalEvidence(c ct.Result) int {
	total := 0
	for _, oo := range c.Outcome.PerOp {
		total += oo.EvidenceCount()
	}
	return total
}
