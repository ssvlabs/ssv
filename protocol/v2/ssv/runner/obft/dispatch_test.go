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

// TestController_LRUEvictsOldestSlot — when MaxPendingSlots is exceeded,
// the oldest slot (FIFO insertion order) is evicted, NOT the slot we're
// inserting into. Ensures that a flood of envelopes at distinct slot
// numbers doesn't grow memory unbounded. Also verifies the three auxiliary
// data structures (pending, pendingElem, pendingOrder) stay in sync.
func TestController_LRUEvictsOldestSlot(t *testing.T) {
	ctrl := newMinimalControllerForTest(t)

	checkInvariant := func(stage string) {
		require.Equal(t, len(ctrl.pending), len(ctrl.pendingElem),
			"%s: pending and pendingElem must have equal cardinality", stage)
		require.Equal(t, len(ctrl.pending), ctrl.pendingOrder.Len(),
			"%s: pending and pendingOrder must have equal cardinality", stage)
		for s := range ctrl.pending {
			elem, ok := ctrl.pendingElem[s]
			require.Truef(t, ok, "%s: slot %d in pending but not pendingElem", stage, s)
			require.Equal(t, s, elem.Value.(phase0.Slot),
				"%s: pendingElem[%d] points to wrong slot", stage, s)
		}
	}

	// Buffer envelopes for slots 0..MaxPendingSlots-1.
	for s := phase0.Slot(0); s < phase0.Slot(MaxPendingSlots); s++ {
		ctrl.BufferEnvelope(s, PendingEnvelope{
			Commit: &obftcore.Commit{Height: obftcore.Height(s)},
		})
	}
	require.Len(t, ctrl.pending, MaxPendingSlots)
	checkInvariant("after fill")

	// One more (slot MaxPendingSlots) → should evict slot 0.
	ctrl.BufferEnvelope(phase0.Slot(MaxPendingSlots), PendingEnvelope{
		Commit: &obftcore.Commit{Height: obftcore.Height(MaxPendingSlots)},
	})
	require.Len(t, ctrl.pending, MaxPendingSlots)
	_, hasSlot0 := ctrl.pending[phase0.Slot(0)]
	require.False(t, hasSlot0, "slot 0 (oldest) should have been evicted")
	_, hasNewSlot := ctrl.pending[phase0.Slot(MaxPendingSlots)]
	require.True(t, hasNewSlot, "newest slot should be present")
	checkInvariant("after eviction")

	// Drain a slot mid-list — invariant must hold.
	ctrl.DrainPending(phase0.Slot(50))
	require.Len(t, ctrl.pending, MaxPendingSlots-1)
	checkInvariant("after drain")

	// Forget another slot.
	ctrl.ForgetPending(phase0.Slot(100))
	require.Len(t, ctrl.pending, MaxPendingSlots-2)
	checkInvariant("after forget")
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

// Regression: state-mutating methods on a Controller for which EndInstance
// has run must return ErrNoActiveInstance instead of silently mutating an
// orphaned instance. Covers two paths:
//  1. Post-delete: instance removed from c.instances; lookup itself returns
//     ErrNoActiveInstance. (Easy case.)
//  2. Post-finalize, pre-delete: lookup succeeds (instance still in map) but
//     Instance.Ended() returns true; the per-method Ended() gate refuses
//     mutation. This is the actual race window — a goroutine that captured
//     r via lookup before EndInstance ran would have its acquire-mutex-then-
//     mutate sequence fenced here. Simulated by manually calling Finalize
//     on the running instance without going through EndInstance.
func TestController_PostEndInstance_RejectsMutations(t *testing.T) {
	ctrl := newMinimalControllerForStartTest(t)
	const slot phase0.Slot = 100
	r, err := ctrl.StartNewInstance(slot)
	require.NoError(t, err)

	commit := &obftcore.Commit{
		ClusterID:  ctrl.clusterID,
		OperatorID: 2,
		Height:     obftcore.Height(slot),
		Layers:     make([]obftcore.EncryptedLayer, 4),
	}
	bundle := &obftcore.Phase1Bundle{
		ClusterID:  ctrl.clusterID,
		OperatorID: 1,
		Height:     obftcore.Height(slot),
		Layer:      0,
		Value:      []byte("V"),
		SigmaV:     []byte("sig"),
	}

	// Path 2: simulate the race window — manually finalize the instance
	// while keeping it in c.instances. Mutating methods must hit the
	// Ended() gate (not the lookup-misses path).
	r.instanceMu.Lock()
	r.instance.Finalize()
	r.instanceMu.Unlock()
	require.ErrorIs(t, ctrl.ProcessCommit(commit), ErrNoActiveInstance,
		"post-Finalize ProcessCommit must hit the Ended() gate")
	require.ErrorIs(t, ctrl.ObservePhase1Bundle(bundle, 0), ErrNoActiveInstance,
		"post-Finalize ObservePhase1Bundle must hit the Ended() gate")

	// Path 1: full EndInstance (delete from map). Lookup itself fails now.
	ctrl.EndInstance(slot)
	require.ErrorIs(t, ctrl.ProcessCommit(commit), ErrNoActiveInstance)
	require.ErrorIs(t, ctrl.ObservePhase1Bundle(bundle, 0), ErrNoActiveInstance)
}

// newMinimalControllerForStartTest is like newMinimalControllerForTest but
// includes the timing overrides StartNewInstance needs to build a Config.
func newMinimalControllerForStartTest(t *testing.T) *Controller {
	t.Helper()
	const q = 3
	signer := obftcore.NewStubSigner(q, []byte{1})
	ibe := obftcore.NewStubIBE(q)
	pubShares := map[obftcore.OperatorID][]byte{
		1: {1}, 2: {2}, 3: {3}, 4: {4},
	}
	overrides := &ConfigOverrides{K: 4}
	ctrl, err := NewController(ControllerOptions{
		OperatorID:    1,
		Committee:     []spectypes.OperatorID{1, 2, 3, 4},
		ClusterID:     [32]byte{0xCA, 0xFE},
		ClusterPubKey: []byte{0xCA, 0xFE},
		PubKeyShares:  pubShares,
		Signer:        signer,
		IBE:           ibe,
		Overrides:     overrides,
	})
	require.NoError(t, err)
	return ctrl
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
