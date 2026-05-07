package reporting_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
)

// TestReporting_End2End — drives the catalog through both protocols, builds
// a Run, renders all three formats, and verifies each contains expected
// content + parses cleanly.
func TestReporting_End2End(t *testing.T) {
	dir := t.TempDir()

	run := reporting.NewRun("Reporting smoke", "Catalog × {OBFT, QBFT} at canonical config")
	base := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}} {
		for _, s := range ct.Catalog {
			r := ct.RunScenarioOnProtocol(t, p, s, base)
			run.AddCell(s.Name, p.Name(), r)
		}
	}
	run.Finalize()

	t.Run("CSV", func(t *testing.T) {
		path := filepath.Join(dir, "report.csv")
		require.NoError(t, reporting.RenderCSV(run, path))
		body := readFile(t, path)
		require.Contains(t, body, "Scenario,Protocol,Outcome", "CSV header")
		require.Contains(t, body, "Healthy,OBFT", "Healthy/OBFT row")
		require.Contains(t, body, "Healthy,QBFT", "Healthy/QBFT row")
	})

	t.Run("Markdown", func(t *testing.T) {
		path := filepath.Join(dir, "report.md")
		require.NoError(t, reporting.RenderMarkdown(run, path))
		body := readFile(t, path)
		require.Contains(t, body, "# Reporting smoke", "MD title")
		require.Contains(t, body, "## Outcome matrix", "MD outcome section")
		require.Contains(t, body, "## Per-cell bandwidth", "MD bandwidth section")
		require.Contains(t, body, "| Healthy |", "MD healthy row")
	})

	t.Run("HTML", func(t *testing.T) {
		path := filepath.Join(dir, "report.html")
		require.NoError(t, reporting.RenderHTML(run, path))
		body := readFile(t, path)
		require.Contains(t, body, "<title>Reporting smoke</title>", "HTML title")
		require.Contains(t, body, "chart.js", "HTML loads chart.js (CDN)")
		require.Contains(t, body, ">Healthy<", "HTML scenario row")
		require.Contains(t, body, "<canvas id=\"bwChart\"", "HTML bandwidth chart")
		require.Contains(t, body, "<canvas id=\"dtChart\"", "HTML decision-time chart")
		// Sanity: HTML well-formed enough — opening <html> and closing </html>.
		require.True(t, strings.HasPrefix(strings.TrimSpace(body), "<!DOCTYPE html>"),
			"HTML starts with doctype")
		require.Contains(t, body, "</html>", "HTML closes")
	})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
