package reporting

import (
	"fmt"
	"os"
	"strings"
)

// RenderBatchMarkdown writes a BatchRun as a Markdown document with
// three summary tables: success rate, decision-time percentiles,
// bandwidth percentiles. Suitable for inclusion in PR descriptions or
// docs/ markdown files; renders nicely on GitHub.
func RenderBatchMarkdown(br *BatchRun, path string) error {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# %s\n\n", br.Title)
	if br.Description != "" {
		sb.WriteString(br.Description)
		sb.WriteString("\n\n")
	}

	fmt.Fprintf(&sb, "**Generated:** %s  \n", br.GeneratedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "**Iterations:** %d per cell  \n", br.Iterations)
	fmt.Fprintf(&sb, "**Wallclock:** %v  \n", br.Wallclock)
	fmt.Fprintf(&sb, "**Matrix:** %d scenarios × %d protocols = %d cells\n\n",
		len(br.Scenarios), len(br.ProtocolNames), len(br.Cells))

	if len(br.SweepDimensions) > 0 {
		sb.WriteString("**Sweep dimensions:**\n")
		for k, v := range br.SweepDimensions {
			fmt.Fprintf(&sb, "- `%s`: %v\n", k, v)
		}
		sb.WriteString("\n")
	}

	// Table 1: success rate.
	sb.WriteString("## Success rate\n\n")
	writeMatrixHeader(&sb, br)
	for _, scenario := range br.Scenarios {
		fmt.Fprintf(&sb, "| %s |", scenario)
		for _, protocol := range br.ProtocolNames {
			cell, ok := br.CellAt(scenario, protocol)
			if !ok || !Applicable(cell) {
				sb.WriteString(" n/a |")
				continue
			}
			fmt.Fprintf(&sb, " %.0f%% |", cell.SuccessRate*100)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Table 2: decision-time P99.
	sb.WriteString("## Decision-time P99 (ms)\n\n")
	writeMatrixHeader(&sb, br)
	for _, scenario := range br.Scenarios {
		fmt.Fprintf(&sb, "| %s |", scenario)
		for _, protocol := range br.ProtocolNames {
			cell, ok := br.CellAt(scenario, protocol)
			if !ok || !Applicable(cell) || cell.DecisionTime.Len() == 0 {
				sb.WriteString(" — |")
				continue
			}
			fmt.Fprintf(&sb, " %d |", int(cell.DecisionTime.Percentile(99)))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Table 3: bandwidth median.
	sb.WriteString("## Bandwidth median (bytes)\n\n")
	writeMatrixHeader(&sb, br)
	for _, scenario := range br.Scenarios {
		fmt.Fprintf(&sb, "| %s |", scenario)
		for _, protocol := range br.ProtocolNames {
			cell, ok := br.CellAt(scenario, protocol)
			if !ok || !Applicable(cell) {
				sb.WriteString(" n/a |")
				continue
			}
			fmt.Fprintf(&sb, " %d |", int(cell.ClusterBandwidth.Median()))
		}
		sb.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("reporting: write batch markdown %s: %w", path, err)
	}
	return nil
}

// writeMatrixHeader writes the `| Scenario | OBFT | QBFT |` style
// header + alignment row for a markdown matrix table.
func writeMatrixHeader(sb *strings.Builder, br *BatchRun) {
	sb.WriteString("| Scenario |")
	for _, p := range br.ProtocolNames {
		fmt.Fprintf(sb, " %s |", p)
	}
	sb.WriteString("\n|---|")
	for range br.ProtocolNames {
		sb.WriteString("---|")
	}
	sb.WriteString("\n")
}
