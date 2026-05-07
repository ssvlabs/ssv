package consensustest_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
)

// TestGenerateReport drives the catalog through both protocols at the
// canonical operating point, then writes HTML / CSV / Markdown reports to
// REPORT_DIR. Skipped unless REPORT_DIR is set — keeps default `go test`
// runs quiet.
//
// Run via:
//
//	make consensustest-report
//
// or:
//
//	REPORT_DIR=./reports go test -run TestGenerateReport ./protocol/v2/consensustest/
func TestGenerateReport(t *testing.T) {
	dir := os.Getenv("REPORT_DIR")
	if dir == "" {
		t.Skip("REPORT_DIR not set; skipping report generation. Run via `make consensustest-report` to populate.")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	run := reporting.NewRun(
		"OBFT vs QBFT — canonical operating point",
		"Catalog matrix at n=4, BTT=200ms, RelayCutoff=4s. Each cell is one (scenario, protocol) sim.",
	)
	base := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}} {
		for _, s := range ct.Catalog {
			r := ct.RunScenarioOnProtocol(t, p, s, base)
			run.AddCell(s.Name, p.Name(), r)
		}
	}
	run.Finalize()

	htmlPath := filepath.Join(dir, "consensustest.html")
	csvPath := filepath.Join(dir, "consensustest.csv")
	mdPath := filepath.Join(dir, "consensustest.md")

	require.NoError(t, reporting.RenderHTML(run, htmlPath))
	require.NoError(t, reporting.RenderCSV(run, csvPath))
	require.NoError(t, reporting.RenderMarkdown(run, mdPath))

	t.Logf("Reports written:")
	t.Logf("  HTML: %s", htmlPath)
	t.Logf("  CSV:  %s", csvPath)
	t.Logf("  MD:   %s", mdPath)
}
