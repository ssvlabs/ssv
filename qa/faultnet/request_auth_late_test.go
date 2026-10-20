package faultnet

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/qa/faults"
)

const lateSlot = phase0.Slot(200)

// lateNetwork is newTestNetwork with a network config whose genesis puts the release point of a
// request-auth packet for lateSlot (its start + requestAuthLateAfter) at now + untilRelease, and a
// clock frozen at now.
func lateNetwork(t *testing.T, now time.Time, untilRelease time.Duration) (*Network, *fakeP2P, *observer.ObservedLogs) {
	t.Helper()
	const slotDuration = 12 * time.Second
	genesis := now.Add(untilRelease - requestAuthLateAfter - time.Duration(lateSlot)*slotDuration)
	n, inner, logs := newTestNetwork(&fakeSigner{})
	n.netCfg = &networkconfig.Network{Beacon: &networkconfig.Beacon{GenesisTime: genesis, SlotDuration: slotDuration}}
	n.clock = func() time.Time { return now }
	return n, inner, logs
}

func firedWithLabel(logs *observer.ObservedLogs, label string) *observer.ObservedLogs {
	return logs.FilterMessageSnippet(fireSnippet).FilterField(zap.String("label", label))
}

func TestPlanRequestAuthLateHoldsEveryType9Packet(t *testing.T) {
	for _, entries := range []int{1, 2, 8} {
		msg := authMsg(t, lateSlot, entries)
		original := append([]byte(nil), msg.SSVMessage.Data...)

		out := Plan(faults.RequestAuthLate, msg, lateSlot, lateSlot-17, 0)

		require.Len(t, out, 1, "entries=%d", entries)
		o := out[0]
		require.Same(t, msg, o.Msg, "the held message is the honest one, not a copy")
		require.Equal(t, original, o.Msg.SSVMessage.Data, "the honest bytes are untouched")
		require.Equal(t, lateSlot, o.Slot)
		require.False(t, o.Resign, "a held packet is honest: it is never re-signed")
		require.Zero(t, o.Delay)
		require.Zero(t, o.DelaySlots)
		require.Equal(t, &Hold{Slot: lateSlot, After: 4 * time.Second}, o.Hold,
			"release at the start of the packet's proposal slot plus 4 s")
		require.False(t, forges(out), "nothing forged, so the budget gate never suppresses the hold")
	}
}

func TestPlanRequestAuthLateLeavesOtherMessagesAlone(t *testing.T) {
	for name, msg := range map[string]*spectypes.SignedSSVMessage{
		"type-8 preference": prefsMsg(t, lateSlot),
		"role-7 partial":    partialMsg(t, spectypes.RolePTCAttester, lateSlot),
		"proposer partial":  partialMsg(t, spectypes.RoleProposer, lateSlot),
		"consensus under role 8": func() *spectypes.SignedSSVMessage {
			m := authMsg(t, lateSlot, 1)
			m.SSVMessage.MsgType = spectypes.SSVConsensusMsgType
			return m
		}(),
	} {
		out := Plan(faults.RequestAuthLate, msg, lateSlot, lateSlot-17, 0)
		require.Len(t, out, 1, name)
		require.Same(t, msg, out[0].Msg, name)
		require.Nil(t, out[0].Hold, name)
	}

	// The other auth values never hold a type-9 packet.
	for _, f := range []faults.Fault{faults.None, faults.Auth9Entries, faults.AuthRepeatRootInPacket} {
		for _, o := range Plan(f, authMsg(t, lateSlot, 1), lateSlot, lateSlot-17, 0) {
			require.Nil(t, o.Hold, f)
		}
	}
}

func TestDispatchHoldsUntilReleasePointThenSendsUnchanged(t *testing.T) {
	faults.SetForTest(t, faults.RequestAuthLate)
	const untilRelease = 300 * time.Millisecond
	signer := &fakeSigner{}
	n, inner, logs := lateNetwork(t, time.Now(), untilRelease)
	n.signer = signer
	msg := authMsg(t, lateSlot, 1)
	original := append([]byte(nil), msg.SSVMessage.Data...)
	plan := Plan(faults.RequestAuthLate, msg, lateSlot, lateSlot-17, 0)

	start := time.Now()
	require.NoError(t, n.dispatch(plan), "the caller sees a successful broadcast while the packet is held")

	held := firedWithLabel(logs, "held")
	require.Equal(t, 1, held.Len(), "one 🧪 line when the packet is held")
	ctx := held.All()[0].ContextMap()
	require.Equal(t, string(faults.RequestAuthLate), ctx["qa_fault"])
	require.EqualValues(t, lateSlot, ctx["slot"])
	require.EqualValues(t, lateSlot, ctx["payload_slot"])
	require.Equal(t, untilRelease, ctx["hold"])
	require.True(t, n.netCfg.SlotStartTime(lateSlot).Add(4*time.Second).Equal(ctx["release_at"].(time.Time)))

	time.Sleep(untilRelease / 2)
	require.Equal(t, 0, inner.count(), "nothing reaches the wire before the release point")
	require.Equal(t, 0, firedWithLabel(logs, "released").Len())

	require.Eventually(t, func() bool { return inner.count() == 1 }, 3*time.Second, 5*time.Millisecond)
	require.GreaterOrEqual(t, time.Since(start), untilRelease, "released no earlier than the release point")
	require.Same(t, msg, inner.at(0), "the honest message itself is released")
	require.Equal(t, original, inner.dataAt(0), "released byte-for-byte unchanged")
	require.Equal(t, 0, signer.callCount(), "a held packet is never re-signed")

	require.Eventually(t, func() bool { return firedWithLabel(logs, "released").Len() == 1 }, time.Second, 5*time.Millisecond,
		"one 🧪 line when the packet is released")
	released := firedWithLabel(logs, "released").All()[0].ContextMap()
	require.Equal(t, string(faults.RequestAuthLate), released["qa_fault"])
	require.EqualValues(t, lateSlot, released["slot"])
	require.Equal(t, 1, inner.count(), "released exactly once")
}

func TestDispatchHoldPastReleasePointSendsAtOnce(t *testing.T) {
	faults.SetForTest(t, faults.RequestAuthLate)
	n, inner, logs := lateNetwork(t, time.Now(), -2*time.Second)
	plan := Plan(faults.RequestAuthLate, authMsg(t, lateSlot, 1), lateSlot, lateSlot, 0)

	require.NoError(t, n.dispatch(plan))

	require.Equal(t, time.Duration(0), firedWithLabel(logs, "held").All()[0].ContextMap()["hold"])
	require.Eventually(t, func() bool { return inner.count() == 1 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return firedWithLabel(logs, "released").Len() == 1 }, time.Second, 5*time.Millisecond,
		"a late emission still logs the held/released pair")
}

func TestDispatchHoldLogsFailedRelease(t *testing.T) {
	faults.SetForTest(t, faults.RequestAuthLate)
	n, inner, logs := lateNetwork(t, time.Now(), 20*time.Millisecond)
	inner.failAt = 1
	plan := Plan(faults.RequestAuthLate, authMsg(t, lateSlot, 1), lateSlot, lateSlot-17, 0)

	require.NoError(t, n.dispatch(plan), "a release failure happens after the caller returned")

	require.Eventually(t, func() bool { return logs.FilterMessageSnippet("held send failed").Len() == 1 },
		time.Second, 5*time.Millisecond)
	require.Equal(t, 0, firedWithLabel(logs, "released").Len(), "no released line for a packet that never went out")
}

// Through the full decorator path (Plan, throttle, dispatch) with the real clock.
func TestBroadcastAtSlotHoldsRequestAuth(t *testing.T) {
	faults.SetForTest(t, faults.RequestAuthLate)
	n, inner, logs := lateNetwork(t, time.Now(), 200*time.Millisecond)
	n.clock = nil
	msg := authMsg(t, lateSlot, 1)

	require.NoError(t, n.BroadcastAtSlot(msg, lateSlot))
	require.Equal(t, 0, inner.count())
	require.Equal(t, 1, firedWithLabel(logs, "held").Len())

	// A type-8 preference for the same slot goes straight out.
	require.NoError(t, n.BroadcastAtSlot(prefsMsg(t, lateSlot), lateSlot))
	require.Equal(t, 1, inner.count())

	require.Eventually(t, func() bool { return inner.count() == 2 }, 3*time.Second, 5*time.Millisecond)
	require.Same(t, msg, inner.at(1))
}
