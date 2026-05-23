package twoab

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	basewire "github.com/ssvlabs/ssv/protocol/v2/obft/base/wire"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	wire "github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
)

func newDispatchSched(t *testing.T) *Scheduler {
	t.Helper()
	ctrl := newTestController(t, 1)
	sched, err := NewScheduler(ctrl, (&captureHooks{}).lifecycle())
	require.NoError(t, err)
	return sched
}

// TestDispatch_BuffersEachKindOnNoActiveInstance — with no instance started for
// the target slot, every one of the five 2ab kinds must buffer (returns nil)
// into the matching PendingEnvelope field rather than erroring or being dropped.
// This pins the kind → field + slot routing for the whole envelope set.
func TestDispatch_BuffersEachKindOnNoActiveInstance(t *testing.T) {
	const slot phase0.Slot = 100
	h := twoabcore.Height(slot)

	cases := []struct {
		name  string
		env   *wire.Envelope
		check func(t *testing.T, e PendingEnvelope)
	}{
		{
			name: "phase1bundle",
			env: &wire.Envelope{Kind: wire.KindPhase1Bundle, Phase1Bundle: &twoabcore.Phase1Bundle{
				OperatorID: 1, Height: h, Layer: 0, Value: twoabcore.Value("V"), LeaderSigma: twoabcore.Signature("s"),
			}},
			check: func(t *testing.T, e PendingEnvelope) { require.NotNil(t, e.Bundle) },
		},
		{
			name:  "value",
			env:   &wire.Envelope{Kind: wire.KindValue, ValueMsg: &twoabcore.ValueMsg{OperatorID: 2, Height: h, V: twoabcore.Value("V")}},
			check: func(t *testing.T, e PendingEnvelope) { require.NotNil(t, e.ValueMsg) },
		},
		{
			name:  "novalue",
			env:   &wire.Envelope{Kind: wire.KindNoValue, NoValueMsg: &twoabcore.NoValueMsg{OperatorID: 2, Height: h}},
			check: func(t *testing.T, e PendingEnvelope) { require.NotNil(t, e.NoValueMsg) },
		},
		{
			name:  "commit",
			env:   &wire.Envelope{Kind: wire.KindCommit, Commit: &twoabcore.Commit{OperatorID: 2, Height: h, Side: twoabcore.CommitSideNR}},
			check: func(t *testing.T, e PendingEnvelope) { require.NotNil(t, e.Commit) },
		},
		{
			name:  "certificate",
			env:   &wire.Envelope{Kind: wire.KindCertificate, Certificate: &twoabcore.Certificate{Height: h, Value: twoabcore.Value("V")}},
			check: func(t *testing.T, e PendingEnvelope) { require.NotNil(t, e.Certificate) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sched := newDispatchSched(t)
			require.NoError(t, DispatchEnvelope(t.Context(), sched, tc.env, 0))
			pending := sched.Controller().DrainPending(slot)
			require.Len(t, pending, 1)
			tc.check(t, pending[0])
		})
	}
}

// TestDispatch_CrossVariantRejected — a bare-OBFT envelope must never be
// misrouted into the 2abOBFT path. base KindPhase1Bundle (0x01) collides with
// twoab's, so the frame parses but the body decoder hits the ProtocolTag
// mismatch ("OBFT-v1\0" vs "2abOBFT…") and errors. This is the load-bearing
// domain separation between the two protocols' wire formats.
func TestDispatch_CrossVariantRejected(t *testing.T) {
	sched := newDispatchSched(t)
	// base.Phase1Bundle and twoab.Phase1Bundle are the same aliased obft type,
	// so a single value wraps via base's encoder to forge a bare-OBFT envelope.
	bare, err := basewire.WrapPhase1Bundle(&twoabcore.Phase1Bundle{
		OperatorID: 1, Height: 100, Layer: 0, Value: twoabcore.Value("V"), LeaderSigma: twoabcore.Signature("s"),
	})
	require.NoError(t, err)
	err = DispatchBytes(t.Context(), sched, bare, 0)
	require.ErrorContains(t, err, "unwrap envelope")
	// Nothing was buffered — the bare envelope is rejected, not stashed.
	require.Empty(t, sched.Controller().DrainPending(100))
}

// TestDispatch_Bytes_TwoabRoundTrips — the positive control for DispatchBytes:
// a genuine 2ab-wrapped envelope unwraps and routes (here, buffers for the
// not-yet-started slot).
func TestDispatch_Bytes_TwoabRoundTrips(t *testing.T) {
	sched := newDispatchSched(t)
	data, err := wire.WrapCommit(&twoabcore.Commit{
		ClusterID: [32]byte{0xAB}, OperatorID: 2, Height: 100, Side: twoabcore.CommitSideNR, L0Partial: twoabcore.Signature("p"),
	})
	require.NoError(t, err)
	require.NoError(t, DispatchBytes(t.Context(), sched, data, 0))
	require.Len(t, sched.Controller().DrainPending(100), 1)
}

func TestDispatch_UnknownKind(t *testing.T) {
	sched := newDispatchSched(t)
	err := DispatchEnvelope(t.Context(), sched, &wire.Envelope{Kind: wire.MessageKind(0x99)}, 0)
	require.ErrorContains(t, err, "unknown envelope kind")
}

func TestDispatch_NilGuards(t *testing.T) {
	sched := newDispatchSched(t)
	require.ErrorContains(t, DispatchEnvelope(t.Context(), nil, &wire.Envelope{}, 0), "nil Scheduler")
	require.ErrorContains(t, DispatchEnvelope(t.Context(), sched, nil, 0), "nil Envelope")
	// Kind set but the matching typed field nil — guarded per kind.
	require.ErrorContains(t, DispatchEnvelope(t.Context(), sched, &wire.Envelope{Kind: wire.KindCommit}, 0), "nil Commit")
	require.ErrorContains(t, DispatchBytes(t.Context(), sched, []byte{0xFF}, 0), "unwrap envelope")
}
