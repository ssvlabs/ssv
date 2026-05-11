package reporting_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
)

// smallScenarios picks a few scenarios for fast smoke tests. Mixes:
//   - Healthy (success both protocols)
//   - Equivocate_111 (miss OBFT, success QBFT)
//   - HV1SelectiveDelivery (success OBFT, n/a QBFT)
func smallScenarios(t *testing.T) []ct.Scenario {
	t.Helper()
	out := []ct.Scenario{}
	wanted := map[string]bool{
		"Healthy":              true,
		"Equivocate_111":       true,
		"HV1SelectiveDelivery": true,
	}
	for _, s := range ct.Catalog {
		if wanted[s.Name] {
			out = append(out, s)
		}
	}
	require.Len(t, out, 3)
	return out
}

func runOneSweep(t *testing.T, name, description, axis string, points []ct.SweepPoint) ct.SweepResult {
	t.Helper()
	return ct.RunSweep(t, ct.Sweep{
		Name: name, Description: description, AxisLabel: axis, Points: points,
	})
}

// TestRenderComparison_AppliesDetailAndTrendLayouts — one 1-point sweep
// (detail layout) + one 2-point sweep (trend layout) → check both kinds
// of sections render and contain expected canvases.
func TestRenderComparison_AppliesDetailAndTrendLayouts(t *testing.T) {
	scenarios := smallScenarios(t)
	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}

	canonical := runOneSweep(t, "canonical", "Reference: n=4 BTT=200ms", "",
		[]ct.SweepPoint{{
			Label: "n=4 BTT=200ms",
			Config: ct.BatchConfig{
				Iterations: 3, SeedStart: 1,
				Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
				Scenarios: scenarios, Protocols: protos,
			},
		}})

	btt := runOneSweep(t, "btt_two", "Network-degradation: BTT ∈ {100, 200}ms", "BTT",
		[]ct.SweepPoint{
			{Label: "BTT=100ms", Config: ct.BatchConfig{
				Iterations: 3, SeedStart: 1,
				Base:      ct.DefaultProposerDutyConfig(100 * time.Millisecond),
				Scenarios: scenarios, Protocols: protos,
			}},
			{Label: "BTT=200ms", Config: ct.BatchConfig{
				Iterations: 3, SeedStart: 2,
				Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
				Scenarios: scenarios, Protocols: protos,
			}},
		})

	path := t.TempDir() + "/comparison.html"
	require.NoError(t, reporting.RenderComparison(reporting.Comparison{
		Title:       "smoke comparison",
		Description: "three scenarios × two protocols",
		Sweeps:      []ct.SweepResult{canonical, btt},
		Iterations:  3,
		Wallclock:   123 * time.Millisecond,
		GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}, path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	body := string(data)

	require.Contains(t, body, "<title>smoke comparison</title>")
	require.Contains(t, body, "three scenarios × two protocols")
	require.Contains(t, body, "Iterations per cell: 3")
	require.Contains(t, body, "2026-05-11 12:00:00", "generated timestamp")

	// TOC links present for both sweeps.
	require.Contains(t, body, `href="#sweep-canonical"`)
	require.Contains(t, body, `href="#sweep-btt_two"`)

	// Section anchors.
	require.Contains(t, body, `id="sweep-canonical"`)
	require.Contains(t, body, `id="sweep-btt_two"`)

	// Canonical sweep → detail panels (4 canvases + summary matrix).
	require.Contains(t, body, "Summary matrix")
	require.Contains(t, body, `id="canonical_success"`)
	require.Contains(t, body, `id="canonical_latency"`)
	require.Contains(t, body, `id="canonical_bandwidth"`)
	require.Contains(t, body, `id="canonical_tradeoff"`)

	// Multi-point sweep → trend panels (3 line charts).
	require.Contains(t, body, `id="btt_two_success"`)
	require.Contains(t, body, `id="btt_two_p99"`)
	require.Contains(t, body, `id="btt_two_bw"`)
	require.Contains(t, body, "type: 'line'", "trend section uses line charts")

	// Human-readable titles appear (not just slugs).
	require.Contains(t, body, "All-honest healthy path", "scenario Title rendered")
	require.Contains(t, body, "Selective delivery: V to f honest only", "HV1Selective Title rendered")

	// X-axis label populated from sweep.AxisLabel.
	require.Contains(t, body, "'BTT'", "trend X-axis label")
}

// TestRenderComparison_NACellsHandled — a scenario n/a for one protocol
// shows "n/a" in the summary matrix and is skipped from line-chart
// datasets (no all-null datasets emitted).
func TestRenderComparison_NACellsHandled(t *testing.T) {
	naScenario := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "HV1SelectiveDelivery" {
			naScenario = append(naScenario, s)
			break
		}
	}
	require.Len(t, naScenario, 1)

	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	canonical := runOneSweep(t, "canonical", "n=4", "",
		[]ct.SweepPoint{{Label: "n=4 BTT=200ms",
			Config: ct.BatchConfig{
				Iterations: 3, SeedStart: 1,
				Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
				Scenarios: naScenario, Protocols: protos,
			}}})

	path := t.TempDir() + "/na.html"
	require.NoError(t, reporting.RenderComparison(reporting.Comparison{
		Title: "na test", Sweeps: []ct.SweepResult{canonical}, Iterations: 3,
		GeneratedAt: time.Now(),
	}, path))
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(body), `class="na">n/a<`, "QBFT cell rendered as n/a")
}

// TestRenderComparison_EmptySweeps — rendering with no sweeps returns
// an error rather than producing an empty page.
func TestRenderComparison_EmptySweeps(t *testing.T) {
	err := reporting.RenderComparison(reporting.Comparison{
		Title: "empty", Sweeps: nil,
	}, t.TempDir()+"/empty.html")
	require.Error(t, err)
}

// TestRenderComparison_DuplicateSweepNames — sweep names become HTML
// element IDs; duplicates would collide silently, so RenderComparison
// rejects them up front.
func TestRenderComparison_DuplicateSweepNames(t *testing.T) {
	scenarios := smallScenarios(t)
	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	one := runOneSweep(t, "same", "first", "", []ct.SweepPoint{{
		Label: "n=4", Config: ct.BatchConfig{
			Iterations: 1, SeedStart: 1,
			Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
			Scenarios: scenarios, Protocols: protos,
		}}})
	two := runOneSweep(t, "same", "second", "", []ct.SweepPoint{{
		Label: "n=4", Config: ct.BatchConfig{
			Iterations: 1, SeedStart: 2,
			Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
			Scenarios: scenarios, Protocols: protos,
		}}})

	err := reporting.RenderComparison(reporting.Comparison{
		Title: "dup", Sweeps: []ct.SweepResult{one, two}, Iterations: 1,
		GeneratedAt: time.Now(),
	}, t.TempDir()+"/dup.html")
	require.Error(t, err)
	require.Contains(t, err.Error(), `duplicate sweep name "same"`)
}

// TestRenderComparison_UsesSweepTitle — Sweep.Title is the human-readable
// section heading; DisplayTitle() falls back to Name when Title is empty.
func TestRenderComparison_UsesSweepTitle(t *testing.T) {
	scenarios := smallScenarios(t)
	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	sw := ct.Sweep{
		Name:        "my_slug",
		Title:       "My Pretty Heading",
		Description: "Some longer description.",
		AxisLabel:   "",
		Points: []ct.SweepPoint{{Label: "p", Config: ct.BatchConfig{
			Iterations: 1, SeedStart: 1,
			Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
			Scenarios: scenarios, Protocols: protos,
		}}},
	}
	result := ct.RunSweep(t, sw)

	path := t.TempDir() + "/heading.html"
	require.NoError(t, reporting.RenderComparison(reporting.Comparison{
		Title: "heading test", Sweeps: []ct.SweepResult{result}, Iterations: 1,
		GeneratedAt: time.Now(),
	}, path))
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(body), `<h2>My Pretty Heading</h2>`, "section uses Title")
	require.Contains(t, string(body), `href="#sweep-my_slug">My Pretty Heading</a>`, "TOC uses Title")
}

// TestApplicable_ZeroIterationsIsFalse — sanity check the helper used by
// renderers to treat scenario-not-applicable cells as n/a.
func TestApplicable_ZeroIterationsIsFalse(t *testing.T) {
	require.False(t, reporting.Applicable(ct.BatchCell{Iterations: 0}))
	require.True(t, reporting.Applicable(ct.BatchCell{Iterations: 1}))
}
