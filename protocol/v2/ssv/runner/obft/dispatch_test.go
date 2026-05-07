package obft

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/wire"
)

// TestController_BufferAndDrainPending — envelopes buffered before
// StartNewInstance must be drainable after the instance is created. This is
// the basic mechanism behind reviewer-finding #7 (pre-instance message
// buffer).
func TestController_BufferAndDrainPending(t *testing.T) {
	ctrl := newMinimalControllerForTest(t)

	const slot phase0.Slot = 100
	bundle := &obftcore.Phase1Bundle{
		ClusterID:  ctrl.clusterID,
		OperatorID: 1,
		Height:     obftcore.Height(slot),
		Layer:      0,
		Value:      []byte("V"),
		SigmaV:     []byte("sig"),
	}
	commit := &obftcore.Commit{
		ClusterID:  ctrl.clusterID,
		OperatorID: 2,
		Height:     obftcore.Height(slot),
		Layers:     make([]obftcore.EncryptedLayer, 4),
	}

	ctrl.BufferEnvelope(slot, PendingEnvelope{Bundle: bundle})
	ctrl.BufferEnvelope(slot, PendingEnvelope{Commit: commit})

	pending := ctrl.DrainPending(slot)
	require.Len(t, pending, 2)
	require.NotNil(t, pending[0].Bundle)
	require.NotNil(t, pending[1].Commit)

	// After draining, the buffer is empty.
	require.Empty(t, ctrl.DrainPending(slot))
}

// TestController_BufferCappedPerSlot — under abuse, the buffer caps at
// MaxPendingPerSlot and drops further envelopes.
func TestController_BufferCappedPerSlot(t *testing.T) {
	ctrl := newMinimalControllerForTest(t)

	const slot phase0.Slot = 100
	for i := 0; i < MaxPendingPerSlot+10; i++ {
		ctrl.BufferEnvelope(slot, PendingEnvelope{
			Commit: &obftcore.Commit{Height: obftcore.Height(slot)},
		})
	}
	require.Len(t, ctrl.DrainPending(slot), MaxPendingPerSlot)
}

// TestController_ForgetPending — explicit cleanup of un-drained slots.
func TestController_ForgetPending(t *testing.T) {
	ctrl := newMinimalControllerForTest(t)

	const slot phase0.Slot = 100
	ctrl.BufferEnvelope(slot, PendingEnvelope{
		Commit: &obftcore.Commit{Height: obftcore.Height(slot)},
	})
	require.NotEmpty(t, ctrl.pending[slot])

	ctrl.ForgetPending(slot)
	require.Empty(t, ctrl.pending[slot])
}

// TestDispatch_BuffersOnNoActiveInstance — when DispatchEnvelope is called
// for a slot with no active instance, the envelope is buffered (returns nil)
// rather than erroring. The reviewer's #7 finding: previously, gossipsub
// peers broadcasting before the local pre-consensus completes had their
// messages dropped permanently.
func TestDispatch_BuffersOnNoActiveInstance(t *testing.T) {
	ctrl := newMinimalControllerForTest(t)
	hooks := &LifecycleHooks{
		FetchCandidate:       func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) { return nil, nil },
		HostValidate:         func(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error) { return true, nil },
		Broadcast:            func(ctx context.Context, slot phase0.Slot, data []byte) error { return nil },
		SubmitOutput:         func(ctx context.Context, slot phase0.Slot, out *obftcore.Output) error { return nil },
		BroadcastCertificate: func(ctx context.Context, slot phase0.Slot, data []byte) error { return nil },
	}
	sched, err := NewScheduler(ctrl, hooks)
	require.NoError(t, err)

	const slot phase0.Slot = 200
	commit := &obftcore.Commit{
		ClusterID:  ctrl.clusterID,
		OperatorID: 2,
		Height:     obftcore.Height(slot),
		Layers:     make([]obftcore.EncryptedLayer, 4),
	}

	// Dispatch with no instance for slot 200 — must buffer, not error.
	err = DispatchEnvelope(t.Context(), sched, &wire.Envelope{Kind: wire.KindCommit, Commit: commit}, 0)
	require.NoError(t, err)
	require.Len(t, ctrl.pending[slot], 1)
}

// newMinimalControllerForTest constructs a Controller with stub crypto and
// minimal config, sufficient for testing the buffer plumbing.
func newMinimalControllerForTest(t *testing.T) *Controller {
	t.Helper()
	const q = 3
	signer := obftcore.NewStubSigner(q, []byte{1})
	ibe := obftcore.NewStubIBE(q)
	pubShares := map[obftcore.OperatorID][]byte{
		1: {1}, 2: {2}, 3: {3}, 4: {4},
	}
	ctrl, err := NewController(ControllerOptions{
		OperatorID:    1,
		Committee:     []spectypes.OperatorID{1, 2, 3, 4},
		ClusterID:     [32]byte{0xCA, 0xFE},
		ClusterPubKey: []byte{0xCA, 0xFE},
		PubKeyShares:  pubShares,
		Signer:        signer,
		IBE:           ibe,
	})
	require.NoError(t, err)
	return ctrl
}
