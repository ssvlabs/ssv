package reporting

import (
	"fmt"
	"os"
	"strings"
)

// RenderMarkdown writes the run as a Markdown document with a header,
// metadata block, and a (scenario × protocol) outcome table. Suitable for
// inclusion in CONSENSUS-TEST-PLAN.md or PR descriptions.
func RenderMarkdown(r *Run, path string) error {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# %s\n\n", r.Title)
	if r.Description != "" {
		sb.WriteString(r.Description)
		sb.WriteString("\n\n")
	}

	fmt.Fprintf(&sb, "**Started:** %s  \n", r.StartedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "**Duration:** %v  \n", r.Duration)
	fmt.Fprintf(&sb, "**Cells:** %d (%d scenarios × %d protocols)\n\n",
		len(r.Cells), len(r.Scenarios), len(r.ProtocolNames))

	if len(r.SweepDimensions) > 0 {
		sb.WriteString("**Sweep dimensions:**\n")
		for k, v := range r.SweepDimensions {
			fmt.Fprintf(&sb, "- `%s`: %v\n", k, v)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Outcome matrix\n\n")
	sb.WriteString("| Scenario |")
	for _, p := range r.ProtocolNames {
		fmt.Fprintf(&sb, " %s |", p)
	}
	sb.WriteString("\n|---|")
	for range r.ProtocolNames {
		sb.WriteString("---|")
	}
	sb.WriteString("\n")

	for _, scenario := range r.Scenarios {
		fmt.Fprintf(&sb, "| %s |", scenario)
		for _, protocol := range r.ProtocolNames {
			cell, ok := r.Cells[CellKey{Scenario: scenario, Protocol: protocol}]
			if !ok {
				sb.WriteString(" — |")
				continue
			}
			fmt.Fprintf(&sb, " %s |", cellSummary(cell))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n## Per-cell bandwidth\n\n")
	sb.WriteString("| Scenario |")
	for _, p := range r.ProtocolNames {
		fmt.Fprintf(&sb, " %s |", p)
	}
	sb.WriteString("\n|---|")
	for range r.ProtocolNames {
		sb.WriteString("---|")
	}
	sb.WriteString("\n")

	for _, scenario := range r.Scenarios {
		fmt.Fprintf(&sb, "| %s |", scenario)
		for _, protocol := range r.ProtocolNames {
			cell, ok := r.Cells[CellKey{Scenario: scenario, Protocol: protocol}]
			if !ok || cell.Skipped {
				sb.WriteString(" — |")
				continue
			}
			fmt.Fprintf(&sb, " %dB |", cell.Outcome.Bandwidth.TotalBytes)
		}
		sb.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("reporting: write markdown %s: %w", path, err)
	}
	return nil
}

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
