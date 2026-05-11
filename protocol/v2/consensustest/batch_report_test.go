package consensustest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
)

// TestGenerateBatchReport runs the curated DefaultSweeps over the full
// catalog with both protocols and writes per-(sweep, point) HTML / CSV /
// Markdown reports plus a top-level index.html.
//
// Gated on the REPORT_DIR env var so default `go test` runs stay quiet.
// Iteration count tunable via ITERATIONS env (default 100).
//
// Usage:
//
//	make consensustest-report
//
// Or directly:
//
//	REPORT_DIR=./reports ITERATIONS=100 \
//	    go test -timeout 30m -run TestGenerateBatchReport \
//	    ./protocol/v2/consensustest/
//
// Estimated runtime at default 100 iterations:
//
//	canonical          ~4s    (25 scenarios × 2 protocols × 100 sims)
//	cluster_scaling    ~25s   (× 4 cluster sizes)
//	btt_degradation    ~16s   (× 4 BTT values)
//	heavy_tail         ~16s   (× 4 sigmas)
//	loss               ~16s   (× 4 loss rates)
//	TOTAL              ~75s   on a typical dev machine.
//
// At ITERATIONS=1000, scale linearly (~12-15 min). Above that, consider
// parallelizing across sweeps (currently sequential — per-batch is
// already parallelized internally).
func TestGenerateBatchReport(t *testing.T) {
	dir := os.Getenv("REPORT_DIR")
	if dir == "" {
		t.Skip("REPORT_DIR not set; skipping report generation. Run via `make consensustest-report` to populate.")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	iterations := 100
	if v := os.Getenv("ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "invalid ITERATIONS=%q", v)
		require.Greater(t, n, 0, "ITERATIONS must be > 0")
		iterations = n
	}

	scenarios := ct.Catalog
	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	sweeps := ct.DefaultSweeps(scenarios, protocols, iterations)
	require.Len(t, sweeps, 5)

	var indexEntries []reporting.SweepIndexEntry
	totalStart := time.Now()

	for _, sw := range sweeps {
		swDir := filepath.Join(dir, sw.Name)
		require.NoError(t, os.MkdirAll(swDir, 0o755))

		t.Logf("--- sweep %s (%d points × %d iterations × %d cells)",
			sw.Name, len(sw.Points), iterations, len(scenarios)*len(protocols))
		swStart := time.Now()
		result := ct.RunSweep(t, sw)
		t.Logf("    %s wallclock: %v", sw.Name, time.Since(swStart))

		for i, rep := range result.Reports {
			pt := result.Sweep.Points[i]
			ptSlug := sanitizeFilename(pt.Label)
			title := fmt.Sprintf("%s — %s", sw.Name, pt.Label)
			br := reporting.NewBatchRun(title, sw.Description, rep)
			if sw.AxisLabel != "" {
				br.SweepDimensions[sw.AxisLabel] = []string{pt.Label}
			}

			htmlPath := filepath.Join(swDir, ptSlug+".html")
			csvPath := filepath.Join(swDir, ptSlug+".csv")
			mdPath := filepath.Join(swDir, ptSlug+".md")
			require.NoError(t, reporting.RenderBatchHTML(br, htmlPath))
			require.NoError(t, reporting.RenderBatchCSV(br, csvPath))
			require.NoError(t, reporting.RenderBatchMarkdown(br, mdPath))

			rel := func(p string) string {
				rp, err := filepath.Rel(dir, p)
				if err != nil {
					return p
				}
				return rp
			}
			indexEntries = append(indexEntries, reporting.SweepIndexEntry{
				SweepName:        sw.Name,
				SweepDescription: sw.Description,
				AxisLabel:        sw.AxisLabel,
				PointLabel:       pt.Label,
				HTMLPath:         rel(htmlPath),
				CSVPath:          rel(csvPath),
				MarkdownPath:     rel(mdPath),
			})
		}
	}

	indexPath := filepath.Join(dir, "index.html")
	require.NoError(t, reporting.RenderSweepIndex(
		"consensustest batch comparison",
		fmt.Sprintf("Five curated sweeps × OBFT/QBFT × %d iterations. Total wallclock: %v.",
			iterations, time.Since(totalStart)),
		indexEntries,
		indexPath,
	))

	t.Logf("Index written: %s", indexPath)
	t.Logf("Total entries: %d", len(indexEntries))
	t.Logf("Total wallclock: %v", time.Since(totalStart))
}

// sanitizeFilename returns a filesystem-safe version of label suitable
// for use as a filename. Replaces spaces / equals / slashes / etc. with
// underscores and trims leading/trailing junk.
func sanitizeFilename(label string) string {
	repl := strings.NewReplacer(
		" ", "_",
		"=", "_",
		"/", "_",
		":", "_",
		"·", "_",
	)
	out := repl.Replace(label)
	out = strings.Trim(out, "_")
	if out == "" {
		return "unnamed"
	}
	return out
}
