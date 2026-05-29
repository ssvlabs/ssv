package twoab

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// newTestController builds a Controller for local operator `op` in a 4-operator
// cluster, using the non-cryptographic StubSigner/StubIBE (protocol-level
// testing; the real-BLS path is covered by the L6 end-to-end test).
func newTestController(t *testing.T, op spectypes.OperatorID) *Controller {
	t.Helper()
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	// Reference cfg only to read QV/QEnc for stub construction; the Controller
	// rebuilds its own cfg per slot in StartNewInstance.
	ref, err := ConfigForCluster(0, committee, [32]byte{}, nil)
	require.NoError(t, err)

	pubShares := make(map[twoabcore.OperatorID][]byte, len(committee))
	for _, o := range committee {
		pubShares[twoabcore.OperatorID(o)] = []byte{byte(o)}
	}

	ctrl, err := NewController(ControllerOptions{
		OperatorID:    op,
		Committee:     committee,
		ClusterID:     [32]byte{},
		ClusterPubKey: []byte{0xCC, 0xDD}, // stub VerifyAggregate ignores the value
		PubKeyShares:  pubShares,
		Signer:        obft.NewStubSigner(ref.QV(), []byte{byte(op)}),
		TagSigner:     obft.NewStubSigner(ref.QV(), []byte{byte(op)}),
		IBE:           obft.NewStubIBE(ref.QEnc()),
	})
	require.NoError(t, err)
	return ctrl
}

func TestNewController_Validation(t *testing.T) {
	base := ControllerOptions{
		OperatorID:    1,
		Committee:     []spectypes.OperatorID{1, 2, 3, 4},
		ClusterPubKey: []byte{0x01},
		PubKeyShares:  map[twoabcore.OperatorID][]byte{1: {1}},
		Signer:        obft.NewStubSigner(2, []byte{1}),
		IBE:           obft.NewStubIBE(3),
	}
	// Sanity: the base is valid.
	_, err := NewController(base)
	require.NoError(t, err)

	mut := func(f func(o *ControllerOptions)) ControllerOptions { o := base; f(&o); return o }
	_, err = NewController(mut(func(o *ControllerOptions) { o.Signer = nil }))
	require.ErrorContains(t, err, "nil Signer")
	_, err = NewController(mut(func(o *ControllerOptions) { o.IBE = nil }))
	require.ErrorContains(t, err, "nil IBE")
	_, err = NewController(mut(func(o *ControllerOptions) { o.PubKeyShares = nil }))
	require.ErrorContains(t, err, "nil PubKeyShares")
	_, err = NewController(mut(func(o *ControllerOptions) { o.Committee = nil }))
	require.ErrorContains(t, err, "empty committee")
}

func TestController_Lifecycle(t *testing.T) {
	ctrl := newTestController(t, 1)
	const slot = phase0.Slot(0)

	r, err := ctrl.StartNewInstance(slot)
	require.NoError(t, err)
	require.NotNil(t, r)
	require.Equal(t, []int{0}, r.LeaderAtLayers, "op 1 leads layer 0 at slot 0 (sorted[0])")

	_, err = ctrl.StartNewInstance(slot)
	require.ErrorContains(t, err, "already running", "double-start rejected")

	got, ok := ctrl.GetInstance(slot)
	require.True(t, ok)
	require.Same(t, r, got)
	require.Equal(t, []phase0.Slot{slot}, ctrl.ActiveSlots())

	ctrl.EndInstance(slot)
	_, ok = ctrl.GetInstance(slot)
	require.False(t, ok, "instance removed after EndInstance")
	require.Empty(t, ctrl.ActiveSlots())

	// Post-end, a state-touching delegate routes to ErrNoActiveInstance.
	_, err = ctrl.Resolve(slot)
	require.ErrorIs(t, err, ErrNoActiveInstance)
}

// TestController_OnInstanceEnd_Contract pins the production observation hook's
// ordering contract — the only behavior the deterministic-trace-capture fix
// added to production. EndInstance must fire onInstanceEnd exactly once per
// teardown, after the instance is detached from the map and after c.mu is
// released, with the instance Finalized (Ended()==true), passing the same
// RunningInstance StartNewInstance returned. A future EndInstance refactor that
// fired the hook under c.mu, before the map delete, or before Finalize would
// break the race-safety bridge's capture determinism; this fails it locally
// instead, without the full stress harness.
func TestController_OnInstanceEnd_Contract(t *testing.T) {
	ctrl := newTestController(t, 1)
	const slot = phase0.Slot(0)

	var (
		fires            int
		capturedSlot     phase0.Slot
		capturedR        *RunningInstance
		capturedEnded    bool
		capturedDetached bool
	)
	ctrl.onInstanceEnd = func(s phase0.Slot, r *RunningInstance) {
		fires++
		capturedSlot = s
		capturedR = r
		capturedEnded = r.instance.Ended()
		// A c.mu-taking accessor here neither deadlocks (the hook fires after
		// c.mu is released) nor finds the slot (it fires after the map delete).
		capturedDetached = len(ctrl.ActiveSlots()) == 0
	}

	started, err := ctrl.StartNewInstance(slot)
	require.NoError(t, err)

	ctrl.EndInstance(slot)
	require.Equal(t, 1, fires, "hook fires exactly once per teardown")
	require.Same(t, started, capturedR, "hook receives the RunningInstance StartNewInstance returned")
	require.Equal(t, slot, capturedSlot)
	require.True(t, capturedEnded, "instance must be Finalized (Ended) when the hook runs")
	require.True(t, capturedDetached, "hook must fire after the map delete and after c.mu is released")

	// Idempotent EndInstance must not re-fire the hook.
	ctrl.EndInstance(slot)
	require.Equal(t, 1, fires, "second EndInstance is a no-op — the hook does not re-fire")
}

func TestController_DelegateRouting_NoInstance(t *testing.T) {
	ctrl := newTestController(t, 1)
	const slot = phase0.Slot(5) // never started

	_, err := ctrl.BuildPhase1Bundle(slot, 0, []byte("V"))
	require.ErrorIs(t, err, ErrNoActiveInstance)
	require.ErrorIs(t, ctrl.ApplyHostValidity(slot, 0, []byte("V"), true), ErrNoActiveInstance)
	_, _, _, err = ctrl.FirePhase2a(slot)
	require.ErrorIs(t, err, ErrNoActiveInstance)
	_, err = ctrl.Resolve(slot)
	require.ErrorIs(t, err, ErrNoActiveInstance)

	// Routing-by-Height for the message processors (Height == slot).
	require.ErrorIs(t, ctrl.ProcessValueMsg(&twoabcore.ValueMsg{Height: twoabcore.Height(slot)}), ErrNoActiveInstance)
	require.ErrorIs(t, ctrl.ProcessNoValueMsg(&twoabcore.NoValueMsg{Height: twoabcore.Height(slot)}), ErrNoActiveInstance)
	require.ErrorIs(t, ctrl.ProcessCommit(&twoabcore.Commit{Height: twoabcore.Height(slot)}), ErrNoActiveInstance)
	require.ErrorIs(t, ctrl.ProcessCertificate(&twoabcore.Certificate{Height: twoabcore.Height(slot)}), ErrNoActiveInstance)
}

func TestController_DelegateRouting_NilArgs(t *testing.T) {
	ctrl := newTestController(t, 1)
	require.ErrorContains(t, ctrl.ObservePhase1Bundle(nil, 0), "nil phase-1 bundle")
	require.ErrorContains(t, ctrl.ProcessValueMsg(nil), "nil value msg")
	require.ErrorContains(t, ctrl.ProcessNoValueMsg(nil), "nil no-value msg")
	require.ErrorContains(t, ctrl.ProcessCommit(nil), "nil commit")
	require.ErrorContains(t, ctrl.ProcessCertificate(nil), "nil certificate")
}

// TestController_DelegateSurface drives the live delegate surface against a
// started instance (stub crypto). A single operator can't reach σ-quorum, so
// Resolve returns an error — the point is the Controller routes each call into
// the Instance and surfaces results, not protocol convergence (that's L6).
func TestController_DelegateSurface(t *testing.T) {
	ctrl := newTestController(t, 1)
	const slot = phase0.Slot(0) // op 1 leads layer 0

	_, err := ctrl.StartNewInstance(slot)
	require.NoError(t, err)

	v := []byte("V0")
	bundle, err := ctrl.BuildPhase1Bundle(slot, 0, v)
	require.NoError(t, err)
	require.NotNil(t, bundle)

	require.NoError(t, ctrl.ObservePhase1Bundle(bundle, 0))
	require.NoError(t, ctrl.ApplyHostValidity(slot, 0, v, true))

	// Fire Phase 2a: with a host-valid retained own bundle at L_0, op 1 is
	// σ-eligible and emits KindValue.
	val, nv, cm, err := ctrl.FirePhase2a(slot)
	require.NoError(t, err)
	require.NotNil(t, val, "σ-eligible op emits KindValue")
	require.Nil(t, nv)
	require.Nil(t, cm)

	// The own emission is observable for the scheduler's broadcast detection.
	own, err := ctrl.OwnValueMsg(slot)
	require.NoError(t, err)
	require.NotNil(t, own)

	// Single operator → no σ-quorum, no NR-quorum: Resolve fails cleanly.
	out, err := ctrl.Resolve(slot)
	require.Error(t, err)
	require.Nil(t, out)

	// Read-only accessors work on the live instance.
	ev, err := ctrl.Evidence(slot)
	require.NoError(t, err)
	require.Empty(t, ev, "no evidence on the honest single-op path")
}

func TestController_PendingBuffer(t *testing.T) {
	ctrl := newTestController(t, 1)
	const slot = phase0.Slot(9)

	ctrl.BufferEnvelope(slot, PendingEnvelope{Bundle: &twoabcore.Phase1Bundle{Height: twoabcore.Height(slot)}})
	ctrl.BufferEnvelope(slot, PendingEnvelope{ValueMsg: &twoabcore.ValueMsg{Height: twoabcore.Height(slot)}})
	ctrl.BufferEnvelope(slot, PendingEnvelope{NoValueMsg: &twoabcore.NoValueMsg{Height: twoabcore.Height(slot)}})

	drained := ctrl.DrainPending(slot)
	require.Len(t, drained, 3)
	require.NotNil(t, drained[0].Bundle)
	require.NotNil(t, drained[1].ValueMsg)
	require.NotNil(t, drained[2].NoValueMsg)
	require.Empty(t, ctrl.DrainPending(slot), "drained buffer is empty")

	// Per-slot cap.
	for i := 0; i < MaxPendingPerSlot+5; i++ {
		ctrl.BufferEnvelope(slot, PendingEnvelope{Commit: &twoabcore.Commit{Height: twoabcore.Height(slot)}})
	}
	require.Len(t, ctrl.DrainPending(slot), MaxPendingPerSlot)

	// ForgetPending drops without dispatch.
	ctrl.BufferEnvelope(slot, PendingEnvelope{Commit: &twoabcore.Commit{Height: twoabcore.Height(slot)}})
	ctrl.ForgetPending(slot)
	require.Empty(t, ctrl.DrainPending(slot))
}

func TestController_EndedFence(t *testing.T) {
	ctrl := newTestController(t, 1)
	const slot = phase0.Slot(0)

	_, err := ctrl.StartNewInstance(slot)
	require.NoError(t, err)
	ctrl.EndInstance(slot) // marks slot in the ended-ring

	// Post-teardown buffering is refused (nowhere to drain into).
	ctrl.BufferEnvelope(slot, PendingEnvelope{Commit: &twoabcore.Commit{Height: twoabcore.Height(slot)}})
	require.Empty(t, ctrl.DrainPending(slot), "ended slot must refuse buffering")
}

func TestController_ClosedChanOnMiss(t *testing.T) {
	ctrl := newTestController(t, 1)
	const slot = phase0.Slot(123) // never started

	for name, ch := range map[string]<-chan struct{}{
		"StateDeltaChan": ctrl.StateDeltaChan(slot),
		"L0ReadyCh":      ctrl.L0ReadyCh(slot),
	} {
		select {
		case <-ch:
		default:
			t.Fatalf("%s on missing slot must return a pre-closed channel", name)
		}
	}

	wch := ctrl.WantsHostValidationCh(slot)
	select {
	case <-wch:
	default:
		t.Fatal("WantsHostValidationCh on missing slot must return a pre-closed channel")
	}
}

// TestController_LRUEvictsOldestSlot — when MaxPendingSlots is exceeded, the
// oldest slot (FIFO insertion order) is evicted, NOT the slot being inserted
// into, so a flood of envelopes at distinct slot numbers can't grow memory
// unbounded. Also asserts the pending buffer's three internal structures
// (bySlot, elem, order) stay cardinality-synced across fill/evict/drain/forget.
func TestController_LRUEvictsOldestSlot(t *testing.T) {
	ctrl := newTestController(t, 1)

	checkInvariant := func(stage string) {
		require.Equal(t, len(ctrl.pending.bySlot), len(ctrl.pending.elem),
			"%s: bySlot and elem must have equal cardinality", stage)
		require.Equal(t, len(ctrl.pending.bySlot), ctrl.pending.order.Len(),
			"%s: bySlot and order must have equal cardinality", stage)
		for s := range ctrl.pending.bySlot {
			elem, ok := ctrl.pending.elem[s]
			require.Truef(t, ok, "%s: slot %d in bySlot but not elem", stage, s)
			require.Equal(t, s, elem.Value.(phase0.Slot),
				"%s: elem[%d] points to wrong slot", stage, s)
		}
	}

	// Buffer envelopes for slots 0..MaxPendingSlots-1.
	for s := phase0.Slot(0); s < phase0.Slot(MaxPendingSlots); s++ {
		ctrl.BufferEnvelope(s, PendingEnvelope{Commit: &twoabcore.Commit{Height: twoabcore.Height(s)}})
	}
	require.Len(t, ctrl.pending.bySlot, MaxPendingSlots)
	checkInvariant("after fill")

	// One more (slot MaxPendingSlots) → should evict slot 0 (oldest).
	ctrl.BufferEnvelope(phase0.Slot(MaxPendingSlots), PendingEnvelope{
		Commit: &twoabcore.Commit{Height: twoabcore.Height(MaxPendingSlots)},
	})
	require.Len(t, ctrl.pending.bySlot, MaxPendingSlots)
	_, hasSlot0 := ctrl.pending.bySlot[phase0.Slot(0)]
	require.False(t, hasSlot0, "slot 0 (oldest) should have been evicted")
	_, hasNewSlot := ctrl.pending.bySlot[phase0.Slot(MaxPendingSlots)]
	require.True(t, hasNewSlot, "newest slot should be present")
	checkInvariant("after eviction")

	// Drain a slot mid-list — invariant must hold.
	ctrl.DrainPending(phase0.Slot(50))
	require.Len(t, ctrl.pending.bySlot, MaxPendingSlots-1)
	checkInvariant("after drain")

	// Forget another slot.
	ctrl.ForgetPending(phase0.Slot(100))
	require.Len(t, ctrl.pending.bySlot, MaxPendingSlots-2)
	checkInvariant("after forget")
}

// TestController_EndedSlotsRing_Bounded — the ended-slots ring is FIFO-bounded
// at MaxEndedSlots; old slots age out, after which they re-accept pending
// entries (which then sit until LRU eviction, same as a slot that never ended).
// Guards the ring against unbounded growth on a long-running node.
func TestController_EndedSlotsRing_Bounded(t *testing.T) {
	ctrl := newTestController(t, 1)

	// Fill the ring with MaxEndedSlots+1 ended slots; the first must fall out.
	for s := phase0.Slot(0); s < phase0.Slot(MaxEndedSlots+1); s++ {
		_, err := ctrl.StartNewInstance(s)
		require.NoError(t, err)
		ctrl.EndInstance(s)
	}
	ctrl.mu.Lock()
	require.Equal(t, MaxEndedSlots, ctrl.ended.order.Len(),
		"ended ring must be capped at MaxEndedSlots")
	_, slot0Still := ctrl.ended.set[0]
	ctrl.mu.Unlock()
	require.False(t, slot0Still, "slot 0 must have aged out of the ring")

	// slot 0 is no longer fenced — re-buffering allowed again.
	ctrl.BufferEnvelope(phase0.Slot(0), PendingEnvelope{Commit: &twoabcore.Commit{Height: 0}})
	require.Len(t, ctrl.DrainPending(phase0.Slot(0)), 1,
		"slot that aged out of the ring must accept buffering again")
}

// TestController_PostEndInstance_RejectsMutations — state-mutating delegates on
// a Controller whose instance has ended must return ErrNoActiveInstance instead
// of silently mutating an orphaned instance. Covers both fences:
//  1. Post-finalize, pre-delete: lookup succeeds (instance still mapped) but
//     Instance.Ended() is true, so the withLiveInstance Ended() gate refuses
//     the mutation. This is the real race window — a goroutine that captured r
//     via lookup before EndInstance ran is fenced here. Simulated by calling
//     Finalize directly, without going through EndInstance.
//  2. Post-delete: EndInstance removed the instance from the map, so lookup
//     itself misses. (Easy case.)
//
// The value/no-value mutators are 2ab-specific; asserting the gate fences them
// alongside Commit / Phase1Bundle covers the distinctive twoab surface.
func TestController_PostEndInstance_RejectsMutations(t *testing.T) {
	ctrl := newTestController(t, 1)
	const slot = phase0.Slot(0)
	r, err := ctrl.StartNewInstance(slot)
	require.NoError(t, err)

	// Bare-Height stubs suffice: the Ended() gate (path 1) and the lookup miss
	// (path 2) both fire before any message-body validation.
	commit := &twoabcore.Commit{Height: twoabcore.Height(slot)}
	valueMsg := &twoabcore.ValueMsg{Height: twoabcore.Height(slot)}
	noValueMsg := &twoabcore.NoValueMsg{Height: twoabcore.Height(slot)}
	bundle := &twoabcore.Phase1Bundle{Height: twoabcore.Height(slot)}

	// Path 1: simulate the race window — finalize while keeping the instance
	// mapped. Mutators must hit the Ended() gate (not the lookup-miss path).
	r.instanceMu.Lock()
	r.instance.Finalize()
	r.instanceMu.Unlock()
	require.ErrorIs(t, ctrl.ProcessCommit(commit), ErrNoActiveInstance,
		"post-Finalize ProcessCommit must hit the Ended() gate")
	require.ErrorIs(t, ctrl.ProcessValueMsg(valueMsg), ErrNoActiveInstance,
		"post-Finalize ProcessValueMsg must hit the Ended() gate")
	require.ErrorIs(t, ctrl.ProcessNoValueMsg(noValueMsg), ErrNoActiveInstance,
		"post-Finalize ProcessNoValueMsg must hit the Ended() gate")
	require.ErrorIs(t, ctrl.ObservePhase1Bundle(bundle, 0), ErrNoActiveInstance,
		"post-Finalize ObservePhase1Bundle must hit the Ended() gate")

	// Path 2: full EndInstance (delete from map). Lookup itself fails now.
	ctrl.EndInstance(slot)
	require.ErrorIs(t, ctrl.ProcessCommit(commit), ErrNoActiveInstance)
	require.ErrorIs(t, ctrl.ObservePhase1Bundle(bundle, 0), ErrNoActiveInstance)
}
