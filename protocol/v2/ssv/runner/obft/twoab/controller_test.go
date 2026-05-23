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
