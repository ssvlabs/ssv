package consensustest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestCorrectness is the correctness-tier entry point. It runs every
// catalog scenario opted into ModeCorrectness against both protocols at
// the canonical operating point (n=4, BTT=200ms, ConstantDelay,
// single-iteration / fixed seed) and asserts the declared per-protocol
// expectation matches the observed outcome.
//
// Safety invariants are enforced by the framework — any violation panics
// inside RunScenarioOnProtocol via SafetyPanic, terminating the test
// regardless of declared expectation. Per-scenario expectation mismatches
// fail the corresponding sub-test without affecting other scenarios.
//
// One operating point per scenario; no sweeps (sweeps live in the stress
// tier — see TestGenerateBatchReport). Re-running this test produces
// identical results: ConstantDelay is deterministic, seeds are fixed.
//
// See docs/CONSENSUSTEST-SPLIT-PLAN.md.
func TestCorrectness(t *testing.T) {
	profile := ct.CorrectnessProfile(200 * time.Millisecond)
	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}

	scenarios := 0
	for _, s := range ct.Catalog {
		if !s.HasMode(ct.ModeCorrectness) {
			continue
		}
		scenarios++
		t.Run(s.Name, func(t *testing.T) {
			for _, p := range protocols {
				t.Run(p.Name(), func(t *testing.T) {
					r := ct.RunScenarioOnProtocol(t, p, s, profile.BaseConfig)
					require.Truef(t, r.Match,
						"%s/%s expected %s; got: %s",
						p.Name(), s.Name, r.Expected, r.Why)
				})
			}
		})
	}
	require.Greater(t, scenarios, 0, "no catalog scenarios opted into ModeCorrectness")
}
