package obft

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// Controller-surface unit coverage for the bare-OBFT adapter. The pending-buffer,
// ended-fence, LRU-eviction, and post-EndInstance Ended()-gate paths are covered
// in dispatch_test.go; this file covers the remaining surface: NewController
// validation, the StartNewInstance→EndInstance lifecycle, ErrNoActiveInstance
// routing on a never-started slot, the nil-body guards, the live delegate
// surface, and the closed-channel-on-miss accessors. Helpers
// (newMinimalControllerForTest / newMinimalControllerForStartTest) live in
// dispatch_test.go.

// TestNewController_Validation asserts NewController rejects missing
// dependencies. (Happy-path construction is exercised throughout the package
// via the newMinimalController* helpers.)
func TestNewController_Validation(t *testing.T) {
	base := ControllerOptions{
		OperatorID:    1,
		Committee:     []spectypes.OperatorID{1, 2, 3, 4},
		ClusterPubKey: []byte{0x01},
		PubKeyShares:  map[obftcore.OperatorID][]byte{1: {1}},
		Signer:        obftcore.NewStubSigner(2, []byte{1}),
		IBE:           obftcore.NewStubIBE(3),
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

// TestController_Lifecycle drives StartNewInstance → EndInstance: the instance
// registers in ActiveSlots, double-start is rejected, teardown deregisters, and
// a post-teardown state-touching delegate routes to ErrNoActiveInstance.
// Bare-OBFT exposes no GetInstance accessor — presence is observed via
// ActiveSlots; StartNewInstance returns the live *RunningInstance directly.
func TestController_Lifecycle(t *testing.T) {
	ctrl := newMinimalControllerForStartTest(t)
	const slot = phase0.Slot(0)

	r, err := ctrl.StartNewInstance(slot)
	require.NoError(t, err)
	require.NotNil(t, r)
	require.Equal(t, []int{0}, r.LeaderAtLayers, "op 1 leads layer 0 at slot 0 (sorted[0])")
	require.Equal(t, []phase0.Slot{slot}, ctrl.ActiveSlots())

	_, err = ctrl.StartNewInstance(slot)
	require.ErrorContains(t, err, "already running", "double-start rejected")

	ctrl.EndInstance(slot)
	require.Empty(t, ctrl.ActiveSlots(), "instance deregistered after EndInstance")

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
	ctrl := newMinimalControllerForStartTest(t)
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

// TestController_DelegateRouting_NoInstance asserts every state-touching
// delegate returns ErrNoActiveInstance for a slot that was never started — the
// signal the dispatcher uses to buffer-and-replay rather than drop. (The
// complementary post-EndInstance Ended()-gate path is covered by
// TestController_PostEndInstance_RejectsMutations in dispatch_test.go.)
func TestController_DelegateRouting_NoInstance(t *testing.T) {
	ctrl := newMinimalControllerForTest(t)
	const slot = phase0.Slot(5) // never started

	_, err := ctrl.BuildPhase1Bundle(slot, 0, []byte("V"))
	require.ErrorIs(t, err, ErrNoActiveInstance)
	require.ErrorIs(t, ctrl.ApplyHostValidity(slot, 0, []byte("V"), true), ErrNoActiveInstance)
	_, err = ctrl.BuildOwnCommit(slot)
	require.ErrorIs(t, err, ErrNoActiveInstance)
	_, err = ctrl.Resolve(slot)
	require.ErrorIs(t, err, ErrNoActiveInstance)

	// Routing-by-Height for the message-observing delegates (Height == slot).
	// Lookup misses before the body is validated, so bare-Height stubs suffice.
	require.ErrorIs(t, ctrl.ObservePhase1Bundle(&obftcore.Phase1Bundle{Height: obftcore.Height(slot)}, 0), ErrNoActiveInstance)
	require.ErrorIs(t, ctrl.ProcessCommit(&obftcore.Commit{Height: obftcore.Height(slot)}), ErrNoActiveInstance)
	require.ErrorIs(t, ctrl.ProcessCertificate(&obftcore.Certificate{Height: obftcore.Height(slot)}), ErrNoActiveInstance)
}

// TestController_DelegateRouting_NilArgs asserts the nil-body guards on the
// message-observing delegates fire before any routing.
func TestController_DelegateRouting_NilArgs(t *testing.T) {
	ctrl := newMinimalControllerForTest(t)
	require.ErrorContains(t, ctrl.ObservePhase1Bundle(nil, 0), "nil phase-1 bundle")
	require.ErrorContains(t, ctrl.ProcessCommit(nil), "nil commit")
	require.ErrorContains(t, ctrl.ProcessCertificate(nil), "nil certificate")
}

// TestController_DelegateSurface drives the live delegate surface against a
// started instance (stub crypto). A single operator can't reach σ-quorum, so
// Resolve fails — the point is that the Controller forwards each call into the
// Instance and surfaces results/errors, not protocol convergence (that's the
// full-cluster runner tests). Bare-OBFT's Phase-2 emission is the single
// BuildOwnCommit; there is no FirePhase2a / value-msg surface (that's
// 2ab-specific).
func TestController_DelegateSurface(t *testing.T) {
	ctrl := newMinimalControllerForStartTest(t)
	const slot = phase0.Slot(0) // op 1 leads layer 0

	_, err := ctrl.StartNewInstance(slot)
	require.NoError(t, err)

	v := []byte("V0")
	bundle, err := ctrl.BuildPhase1Bundle(slot, 0, v)
	require.NoError(t, err)
	require.NotNil(t, bundle)

	// Re-observing the leader's own (already self-observed) bundle is an
	// idempotent no-op — dedup on the retained value_root — exercising the
	// ObservePhase1Bundle delegate without a second distinct V.
	require.NoError(t, ctrl.ObservePhase1Bundle(bundle, 0))
	require.NoError(t, ctrl.ApplyHostValidity(slot, 0, v, true))

	// Single Phase-2 emission: with a host-valid σ-locked own bundle at L_0,
	// BuildOwnCommit returns a non-nil local commit.
	commit, err := ctrl.BuildOwnCommit(slot)
	require.NoError(t, err)
	require.NotNil(t, commit)

	// Single operator → σ-pool tops out at 1 < qV: Resolve fails cleanly with
	// ErrNoQuorum and yields no Output.
	out, err := ctrl.Resolve(slot)
	require.ErrorIs(t, err, obftcore.ErrNoQuorum)
	require.Nil(t, out)

	// Read-only accessor works on the live instance.
	ev, err := ctrl.Evidence(slot)
	require.NoError(t, err)
	require.Empty(t, ev, "no evidence on the honest single-op path")
}

// TestController_ClosedChanOnMiss asserts the channel accessors return a
// pre-closed channel for a missing slot, so a caller's receive returns
// immediately (and the caller then exits via a follow-up delegate returning
// ErrNoActiveInstance) instead of blocking forever on a stale slot.
func TestController_ClosedChanOnMiss(t *testing.T) {
	ctrl := newMinimalControllerForTest(t)
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
