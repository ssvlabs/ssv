package faultnet

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/qa/faults"
)

func TestBudgetFor(t *testing.T) {
	for _, f := range []faults.Fault{faults.Role7PreFork, faults.Auth9Entries, faults.AuthMixedSigner, faults.VRPreForkSlotReplay} {
		_, ok := budgetFor(f)
		require.True(t, ok, "%s must be throttled", f)
	}
	for _, f := range []faults.Fault{faults.None, faults.TwoEntries, faults.PTCQBFT, faults.Prefs5Roots, faults.PrefsReplay, faults.AuthRepeatRootInPacket} {
		_, ok := budgetFor(f)
		require.False(t, ok, "%s must not be throttled", f)
	}

	// The REJECT-costing values stay inside the per-topic invalid-delivery budget derived in
	// budget.go: rejectsPerSet * maxSets <= maxRejectsPerRun.
	for f, rejectsPerSet := range map[faults.Fault]int{faults.Role7PreFork: 2, faults.Auth9Entries: 1, faults.AuthMixedSigner: 2} {
		b, _ := budgetFor(f)
		require.NotZero(t, b.maxSets, "%s needs a total cap", f)
		require.LessOrEqual(t, rejectsPerSet*b.maxSets, maxRejectsPerRun, "%s", f)
	}
}

func TestGateAllow(t *testing.T) {
	t.Run("per epoch with a cap", func(t *testing.T) {
		g := &gate{}
		b := sendBudget{perEpoch: true, maxSets: 2}
		require.True(t, g.allow(b, 100))  // epoch 3
		require.False(t, g.allow(b, 101)) // same epoch
		require.False(t, g.allow(b, 127))
		require.True(t, g.allow(b, 128))  // epoch 4
		require.False(t, g.allow(b, 160)) // cap reached
		require.True(t, g.exhausted(b))
	})

	t.Run("cap only", func(t *testing.T) {
		g := &gate{}
		b := sendBudget{maxSets: 3}
		for i := 0; i < 3; i++ {
			require.True(t, g.allow(b, 100))
		}
		require.False(t, g.allow(b, 100))
	})

	t.Run("per epoch without a cap never exhausts", func(t *testing.T) {
		g := &gate{}
		b := sendBudget{perEpoch: true}
		for e := phase0.Slot(0); e < 50; e++ {
			require.True(t, g.allow(b, e*slotsPerEpoch+3))
			require.False(t, g.allow(b, e*slotsPerEpoch+4))
		}
		require.False(t, g.exhausted(b))
	})
}

// N triggering broadcasts in one epoch produce exactly one forged set for role7-prefork, and the
// budget-exhausted line is logged once.
func TestThrottleRole7PreFork(t *testing.T) {
	faults.SetForTest(t, faults.Role7PreFork)
	core, logs := observer.New(zapcore.InfoLevel)
	n := &Network{logger: zap.New(core)}

	const preForkSlot = phase0.Slot(95)
	forgedSets := 0
	for i := 0; i < 20; i++ {
		msg := partialMsg(t, spectypes.RolePTCAttester, 200)
		out := n.throttle(faults.Role7PreFork, msg, 200, 200, Plan(faults.Role7PreFork, msg, 200, 200, preForkSlot))
		if len(out) > 1 {
			forgedSets++
		}
		require.Same(t, msg, out[0].Msg, "the honest message always goes out")
	}
	require.Equal(t, 1, forgedSets, "one forged set per epoch")

	// Walk forward until the cap is hit, one epoch at a time.
	b, _ := budgetFor(faults.Role7PreFork)
	for e := 1; e <= b.maxSets+2; e++ {
		now := phase0.Slot(200 + e*slotsPerEpoch)
		msg := partialMsg(t, spectypes.RolePTCAttester, now)
		out := n.throttle(faults.Role7PreFork, msg, now, now, Plan(faults.Role7PreFork, msg, now, now, preForkSlot))
		if len(out) > 1 {
			forgedSets++
		}
	}
	require.Equal(t, b.maxSets, forgedSets)
	require.Equal(t, 1, logs.FilterMessageSnippet("forged-send budget exhausted").Len())
}

func TestThrottleLeavesIdentityPlansAlone(t *testing.T) {
	faults.SetForTest(t, faults.Auth9Entries)
	n := &Network{logger: zap.NewNop()}

	// A type-8 packet: Plan returns identity, which must not consume the budget.
	for i := 0; i < 10; i++ {
		msg := prefsMsg(t, 200)
		out := n.throttle(faults.Auth9Entries, msg, 200, 185, Plan(faults.Auth9Entries, msg, 200, 185, 0))
		require.Len(t, out, 1)
	}
	b, _ := budgetFor(faults.Auth9Entries)
	for i := 0; i < b.maxSets; i++ {
		msg := authMsg(t, 200, 1)
		out := n.throttle(faults.Auth9Entries, msg, 200, 185, Plan(faults.Auth9Entries, msg, 200, 185, 0))
		require.Len(t, out, 4, "set %d is inside the cap", i)
	}
	msg := authMsg(t, 200, 1)
	out := n.throttle(faults.Auth9Entries, msg, 200, 185, Plan(faults.Auth9Entries, msg, 200, 185, 0))
	require.Len(t, out, 1)
	require.Same(t, msg, out[0].Msg)
}

func TestThrottleUnthrottledFaultPassesThrough(t *testing.T) {
	faults.SetForTest(t, faults.TwoEntries)
	n := &Network{logger: zap.NewNop()}
	for i := 0; i < 5; i++ {
		msg := partialMsg(t, spectypes.RolePTCAttester, 200)
		out := n.throttle(faults.TwoEntries, msg, 200, 200, Plan(faults.TwoEntries, msg, 200, 200, 0))
		require.Len(t, out, 2)
	}
}

// send logs the label, the entry count and the payload slot for a forged partial (MSG-02 needs
// payload_slot < 96; the §5 values need the label to confirm every row fired).
func TestSendLogsLabelEntriesAndPayloadSlot(t *testing.T) {
	faults.SetForTest(t, faults.Role7PreFork)
	n, _, logs := newTestNetwork(&fakeSigner{})
	msg := partialMsg(t, spectypes.RolePTCAttester, 95)

	require.NoError(t, n.dispatch([]Outgoing{{Msg: msg, Slot: 130, Resign: true, Label: "x"}}))

	fired := logs.FilterMessageSnippet(fireSnippet).All()
	require.Len(t, fired, 1)
	ctx := fired[0].ContextMap()
	require.EqualValues(t, 130, ctx["slot"])
	require.EqualValues(t, 95, ctx["payload_slot"])
	require.EqualValues(t, 1, ctx["entries"])
	require.Equal(t, "x", ctx["label"])
}
