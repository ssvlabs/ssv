package consensustest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	psigsadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/psigs"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/twoab"
)

// TestTraceGolden pins the exact event sequence (Outcome.Trace) and decided
// outcome for a representative scenario per protocol against committed golden
// files under testdata/.
//
// This is a stronger net than TestDeterminism_AllProtocols: that test only
// proves a run reproduces ITSELF (run1 == run2), so a change that stays
// deterministic but reorders / renames / retimes events would slip past it.
// The golden is a committed baseline, so behavior-preserving-but-DIFFERENT
// drift fails here and forces a conscious regeneration plus a diff review.
//
// Regenerate after an intentional behavior change (and review the diff):
//
//	UPDATE_GOLDEN=1 go test ./protocol/v2/consensustest/ -run TestTraceGolden
//
// Cases run at a fixed seed (DefaultProposerDutyConfig). Direct delivery gives
// compact protocol-logic traces; one mesh case pins the shared desim transport
// event sequence (the Phase-2 extraction).
func TestTraceGolden(t *testing.T) {
	healthy := ct.ByzPattern{Kind: ct.ByzNone}
	silentLeader := ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}

	cases := []struct {
		name     string
		p        ct.Protocol
		byz      ct.ByzPattern
		delivery ct.DeliveryMode
	}{
		{"obft_healthy", obftadapter.Protocol{}, healthy, ct.DeliveryDirect},
		{"obft_silentleader", obftadapter.Protocol{}, silentLeader, ct.DeliveryDirect},
		{"obft_healthy_mesh", obftadapter.Protocol{}, healthy, ct.DeliveryMesh},
		{"twoab_healthy", twoabadapter.Protocol{}, healthy, ct.DeliveryDirect},
		{"twoab_silentleader", twoabadapter.Protocol{}, silentLeader, ct.DeliveryDirect},
		{"qbft_healthy", qbftadapter.QBFT0{}, healthy, ct.DeliveryDirect},
		{"qbft_silentleader", qbftadapter.QBFT0{}, silentLeader, ct.DeliveryDirect},
		{"psigs_healthy", psigsadapter.Protocol{}, healthy, ct.DeliveryDirect},
	}

	update := os.Getenv("UPDATE_GOLDEN") != ""
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
			cfg.Byz = c.byz
			cfg.Delivery = c.delivery
			cfg.TraceEnabled = true

			out, err := c.p.Run(cfg)
			require.NoError(t, err)
			got := renderTraceGolden(out)

			path := filepath.Join("testdata", "trace_"+c.name+".golden")
			if update {
				require.NoError(t, os.MkdirAll("testdata", 0o755))
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
				return
			}
			want, err := os.ReadFile(path)
			require.NoErrorf(t, err,
				"missing golden %s — regenerate: UPDATE_GOLDEN=1 go test ./protocol/v2/consensustest/ -run TestTraceGolden", path)
			require.Equalf(t, string(want), got,
				"%s: trace/outcome drift vs golden. If intentional, regenerate (UPDATE_GOLDEN=1 …) and review the diff.", c.name)
		})
	}
}

// renderTraceGolden serializes an Outcome into the stable golden form: a
// one-line outcome header followed by the (when, event) trace.
func renderTraceGolden(out ct.Outcome) string {
	var b strings.Builder
	fmt.Fprintf(&b, "decided=%v round=%d decisionTime=%v missReason=%q\n",
		out.Decided, out.DecidedRound, out.DecisionTime, out.MissReason)
	b.WriteString("--- trace ---\n")
	for _, e := range out.Trace {
		fmt.Fprintf(&b, "%v\t%s\n", e.When, e.Event)
	}
	return b.String()
}
