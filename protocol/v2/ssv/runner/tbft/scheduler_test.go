package tbft

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/blsbackend"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/wire"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// ---- LifecycleHooks validation -------------------------------------------

func TestNewScheduler_ValidationErrors(t *testing.T) {
	c := newStubController(t, 7)

	// Nil controller.
	_, err := NewScheduler(nil, &LifecycleHooks{})
	require.ErrorContains(t, err, "nil Controller")

	// Nil hooks.
	_, err = NewScheduler(c, nil)
	require.ErrorContains(t, err, "nil LifecycleHooks")

	// Missing required hook fields.
	tests := []struct {
		name    string
		hooks   *LifecycleHooks
		wantErr string
	}{
		{
			name: "missing FetchCandidate",
			hooks: &LifecycleHooks{
				Broadcast:    func(context.Context, phase0.Slot, []byte) error { return nil },
				SubmitOutput: func(context.Context, phase0.Slot, *tbftcore.Output) error { return nil },
			},
			wantErr: "FetchCandidate is required",
		},
		{
			name: "missing Broadcast",
			hooks: &LifecycleHooks{
				FetchCandidate: func(context.Context, phase0.Slot, int) ([]byte, error) { return nil, nil },
				SubmitOutput:   func(context.Context, phase0.Slot, *tbftcore.Output) error { return nil },
			},
			wantErr: "Broadcast is required",
		},
		{
			name: "missing SubmitOutput",
			hooks: &LifecycleHooks{
				FetchCandidate: func(context.Context, phase0.Slot, int) ([]byte, error) { return nil, nil },
				Broadcast:      func(context.Context, phase0.Slot, []byte) error { return nil },
			},
			wantErr: "SubmitOutput is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewScheduler(c, tc.hooks)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// ---- FetchAndBroadcastCandidate ------------------------------------------

func TestScheduler_FetchAndBroadcastCandidate_HappyPath(t *testing.T) {
	c := newStubController(t, 7)
	_, err := c.StartNewInstance(phase0.Slot(100))
	require.NoError(t, err)

	mh := mockHooks()
	mh.fetchValues[layerKey{100, 0}] = []byte("fetched-block-bytes")

	s, err := NewScheduler(c, mh.hooks)
	require.NoError(t, err)

	require.NoError(t, s.FetchAndBroadcastCandidate(context.Background(), phase0.Slot(100), 0))

	// Hook was called.
	require.Equal(t, 1, mh.fetchCalls)
	require.Equal(t, 1, mh.broadcastCalls)

	// Broadcast carried the wrapped CandidateBroadcast with our operator ID.
	require.Len(t, mh.broadcasted, 1)
	env, err := wire.Unwrap(mh.broadcasted[0].data)
	require.NoError(t, err)
	require.Equal(t, wire.KindCandidate, env.Kind)
	require.NotNil(t, env.Candidate)
	require.Equal(t, tbftcore.OperatorID(c.OperatorID()), env.Candidate.OperatorID)
	require.Equal(t, tbftcore.Height(100), env.Candidate.Height)
	require.Equal(t, 0, env.Candidate.Layer)
	require.True(t, bytes.Equal([]byte("fetched-block-bytes"), env.Candidate.Value))
}

func TestScheduler_FetchAndBroadcastCandidate_FetchError(t *testing.T) {
	c := newStubController(t, 7)
	_, err := c.StartNewInstance(phase0.Slot(100))
	require.NoError(t, err)

	mh := mockHooks()
	mh.fetchErr = errors.New("beacon node unreachable")
	s, err := NewScheduler(c, mh.hooks)
	require.NoError(t, err)

	err = s.FetchAndBroadcastCandidate(context.Background(), phase0.Slot(100), 0)
	require.ErrorContains(t, err, "fetch candidate")
	require.ErrorContains(t, err, "beacon node unreachable")
	require.Equal(t, 0, mh.broadcastCalls, "should not broadcast on fetch failure")
}

func TestScheduler_FetchAndBroadcastCandidate_BroadcastError(t *testing.T) {
	c := newStubController(t, 7)
	_, err := c.StartNewInstance(phase0.Slot(100))
	require.NoError(t, err)

	mh := mockHooks()
	mh.fetchValues[layerKey{100, 0}] = []byte("v")
	mh.broadcastErr = errors.New("p2p down")
	s, err := NewScheduler(c, mh.hooks)
	require.NoError(t, err)

	err = s.FetchAndBroadcastCandidate(context.Background(), phase0.Slot(100), 0)
	require.ErrorContains(t, err, "broadcast candidate")
}

// ---- BuildAndBroadcastOnion ---------------------------------------------

func TestScheduler_BuildAndBroadcastOnion_HappyPath(t *testing.T) {
	// Use real BLS so onion construction has actual partial sigs in it.
	threshold.Init()
	const n = 7
	q := 2*((n-1)/3) + 1
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()
	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)
	pubShares := make(map[tbftcore.OperatorID][]byte, n)
	for id, sk := range shares {
		pubShares[tbftcore.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}
	committee := make([]spectypes.OperatorID, n)
	for i := 0; i < n; i++ {
		committee[i] = spectypes.OperatorID(i + 1)
	}

	verifier := blsbackend.New(nil)
	ibe := blsbackend.NewSignerGatedIBE(verifier, masterPub)
	op1Signer := blsbackend.New(shares[1].Serialize())

	c, err := NewController(ControllerOptions{
		OperatorID:    1,
		Committee:     committee,
		ClusterID:     [32]byte{0x01},
		ClusterPubKey: masterPub,
		PubKeyShares:  pubShares,
		Signer:        op1Signer,
		IBE:           ibe,
	})
	require.NoError(t, err)

	const slot = phase0.Slot(50)
	_, err = c.StartNewInstance(slot)
	require.NoError(t, err)

	// Operator 1 has no candidates → onion has all-empty layers, all
	// non-receipts emitted (for layers in [0, K-1)).
	mh := mockHooks()
	s, err := NewScheduler(c, mh.hooks)
	require.NoError(t, err)

	require.NoError(t, s.BuildAndBroadcastOnion(context.Background(), slot))

	// At least 1 onion broadcast + (K-1) non-receipts (since this operator
	// observed no candidates).
	K := 3 // n=7 → K=3
	require.Equal(t, 1+(K-1), mh.broadcastCalls)

	// First broadcast is the Onion.
	env, err := wire.Unwrap(mh.broadcasted[0].data)
	require.NoError(t, err)
	require.Equal(t, wire.KindOnion, env.Kind)

	// Subsequent broadcasts are non-receipts.
	for i := 1; i < len(mh.broadcasted); i++ {
		env, err := wire.Unwrap(mh.broadcasted[i].data)
		require.NoError(t, err)
		require.Equal(t, wire.KindNonReceipt, env.Kind)
	}
}

// ---- ResolveAndSubmit ----------------------------------------------------

func TestScheduler_ResolveAndSubmit_NoQuorum_CallsOnMissedSlot(t *testing.T) {
	// Build a Controller in a state where Resolve will return ErrNoQuorum
	// (no observations made; quorum unreachable).
	c := newStubController(t, 7)
	_, err := c.StartNewInstance(phase0.Slot(100))
	require.NoError(t, err)

	mh := mockHooks()
	missedCalled := 0
	mh.hooks.OnMissedSlot = func(ctx context.Context, slot phase0.Slot, reason error) {
		missedCalled++
	}

	s, err := NewScheduler(c, mh.hooks)
	require.NoError(t, err)

	err = s.ResolveAndSubmit(context.Background(), phase0.Slot(100))
	require.ErrorContains(t, err, "resolve")
	require.Equal(t, 1, missedCalled, "OnMissedSlot must fire on resolve failure")
	require.Equal(t, 0, mh.submitCalls, "SubmitOutput must NOT fire on missed slot")
}

func TestScheduler_ResolveAndSubmit_NoSuchInstance(t *testing.T) {
	c := newStubController(t, 7)
	mh := mockHooks()
	s, err := NewScheduler(c, mh.hooks)
	require.NoError(t, err)

	err = s.ResolveAndSubmit(context.Background(), phase0.Slot(99))
	require.Error(t, err)
}

// ---- Two-operator integration: end-to-end via Scheduler -----------------

// Drive Phase 1 → 2 → 3 across two operators (n=4 cluster, but with only
// 2 operators "alive" — below quorum, so we expect ErrNoQuorum). Validates
// that the Scheduler API composes correctly without orchestrating timing.
func TestScheduler_BelowQuorumCausesMissedSlot(t *testing.T) {
	threshold.Init()
	const n = 4
	q := 2*((n-1)/3) + 1 // q=3
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()
	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)
	pubShares := make(map[tbftcore.OperatorID][]byte, n)
	for id, sk := range shares {
		pubShares[tbftcore.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}
	committee := make([]spectypes.OperatorID, n)
	for i := 0; i < n; i++ {
		committee[i] = spectypes.OperatorID(i + 1)
	}
	verifier := blsbackend.New(nil)
	ibe := blsbackend.NewSignerGatedIBE(verifier, masterPub)

	const slot = phase0.Slot(7)

	// Schedulers for operators 1 and 2 only (operators 3, 4 silent).
	mhs := make(map[spectypes.OperatorID]*mockHooksRecorder, 2)
	scheds := make(map[spectypes.OperatorID]*Scheduler, 2)
	for _, op := range []spectypes.OperatorID{1, 2} {
		// `shares` is keyed by uint64; spectypes.OperatorID has the same
		// underlying type but Go requires the explicit cross-named-type
		// conversion at the index expression.
		opSigner := blsbackend.New(shares[uint64(op)].Serialize()) //nolint:unconvert

		c, err := NewController(ControllerOptions{
			OperatorID:    op,
			Committee:     committee,
			ClusterID:     [32]byte{0xAA},
			ClusterPubKey: masterPub,
			PubKeyShares:  pubShares,
			Signer:        opSigner,
			IBE:           ibe,
		})
		require.NoError(t, err)
		_, err = c.StartNewInstance(slot)
		require.NoError(t, err)

		mh := mockHooks()
		s, err := NewScheduler(c, mh.hooks)
		require.NoError(t, err)
		mhs[op] = mh
		scheds[op] = s
	}

	// Phase 2: each emits its own onion + non-receipts.
	for _, op := range []spectypes.OperatorID{1, 2} {
		require.NoError(t, scheds[op].BuildAndBroadcastOnion(context.Background(), slot))
	}

	// Crosswire the broadcasts: each operator observes the other's messages.
	for _, sender := range []spectypes.OperatorID{1, 2} {
		for _, recv := range []spectypes.OperatorID{1, 2} {
			if sender == recv {
				continue
			}
			for _, msg := range mhs[sender].broadcasted {
				env, err := wire.Unwrap(msg.data)
				require.NoError(t, err)
				switch env.Kind {
				case wire.KindOnion:
					require.NoError(t, scheds[recv].Controller().ProcessOnion(env.Onion))
				case wire.KindNonReceipt:
					require.NoError(t, scheds[recv].Controller().ProcessNonReceipt(env.NonReceipt))
				case wire.KindCandidate:
					require.NoError(t, scheds[recv].Controller().ProcessCandidate(env.Candidate))
				}
			}
		}
	}

	// Phase 3: with only 2 of 4 operators contributing, quorum (3) is
	// unreachable. Expect ErrNoQuorum + OnMissedSlot.
	for _, op := range []spectypes.OperatorID{1, 2} {
		missed := false
		mhs[op].hooks.OnMissedSlot = func(ctx context.Context, slot phase0.Slot, reason error) {
			missed = true
		}
		err := scheds[op].ResolveAndSubmit(context.Background(), slot)
		require.Error(t, err)
		require.True(t, missed, "op %d: OnMissedSlot must fire", op)
	}
}

// ---- mock hooks helper --------------------------------------------------

type layerKey struct {
	slot  phase0.Slot
	layer int
}

type broadcastEvent struct {
	slot phase0.Slot
	data []byte
}

type mockHooksRecorder struct {
	mu sync.Mutex

	hooks *LifecycleHooks

	fetchValues map[layerKey][]byte
	fetchCalls  int
	fetchErr    error

	broadcasted    []broadcastEvent
	broadcastCalls int
	broadcastErr   error

	submittedOutputs []*tbftcore.Output
	submitCalls      int
	submitErr        error
}

// mockHooks returns a fresh recorder + a LifecycleHooks bound to it.
func mockHooks() *mockHooksRecorder {
	r := &mockHooksRecorder{
		fetchValues: make(map[layerKey][]byte),
	}
	r.hooks = &LifecycleHooks{
		FetchCandidate: func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.fetchCalls++
			if r.fetchErr != nil {
				return nil, r.fetchErr
			}
			v, ok := r.fetchValues[layerKey{slot, layer}]
			if !ok {
				return nil, fmt.Errorf("mock hooks: no value configured for slot=%d layer=%d", slot, layer)
			}
			return v, nil
		},
		Broadcast: func(ctx context.Context, slot phase0.Slot, data []byte) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.broadcastCalls++
			if r.broadcastErr != nil {
				return r.broadcastErr
			}
			r.broadcasted = append(r.broadcasted, broadcastEvent{slot: slot, data: append([]byte{}, data...)})
			return nil
		},
		SubmitOutput: func(ctx context.Context, slot phase0.Slot, output *tbftcore.Output) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.submitCalls++
			if r.submitErr != nil {
				return r.submitErr
			}
			r.submittedOutputs = append(r.submittedOutputs, output)
			return nil
		},
	}
	return r
}
