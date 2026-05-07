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
