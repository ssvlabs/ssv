package tbft

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/blsbackend"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// ---- API surface tests ---------------------------------------------------

func TestController_New_ValidationErrors(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	pubShares := map[tbftcore.OperatorID][]byte{1: {1}, 2: {2}, 3: {3}, 4: {4}}

	tests := []struct {
		name    string
		opts    ControllerOptions
		wantErr string
	}{
		{
			name: "nil signer",
			opts: ControllerOptions{
				IBE:          tbftcore.NewStubIBE(3),
				PubKeyShares: pubShares,
				Committee:    committee,
			},
			// Signer left nil → constructor errors.
			wantErr: "nil Signer",
		},
		{
			name: "nil ibe",
			opts: ControllerOptions{
				Signer:       tbftcore.NewStubSigner(3, []byte{1}),
				PubKeyShares: pubShares,
				Committee:    committee,
			},
			wantErr: "nil IBE",
		},
		{
			name: "nil pubkeyshares",
			opts: ControllerOptions{
				Signer:    tbftcore.NewStubSigner(3, []byte{1}),
				IBE:       tbftcore.NewStubIBE(3),
				Committee: committee,
			},
			wantErr: "nil PubKeyShares",
		},
		{
			name: "empty committee",
			opts: ControllerOptions{
				Signer:       tbftcore.NewStubSigner(3, []byte{1}),
				IBE:          tbftcore.NewStubIBE(3),
				PubKeyShares: pubShares,
			},
			wantErr: "empty committee",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewController(tc.opts)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestController_StartNewInstance_Lifecycle(t *testing.T) {
	c := newStubController(t, 7)

	require.Empty(t, c.ActiveSlots())

	r, err := c.StartNewInstance(phase0.Slot(100))
	require.NoError(t, err)
	require.NotNil(t, r)
	require.Equal(t, phase0.Slot(100), r.Slot)
	require.Equal(t, 3, r.Config.K())

	require.Equal(t, []phase0.Slot{100}, c.ActiveSlots())

	// Re-start at the same slot fails.
	_, err = c.StartNewInstance(phase0.Slot(100))
	require.ErrorContains(t, err, "already running")

	// EndInstance removes it; re-start then succeeds.
	c.EndInstance(phase0.Slot(100))
	require.Empty(t, c.ActiveSlots())
	_, err = c.StartNewInstance(phase0.Slot(100))
	require.NoError(t, err)
}

func TestController_LeaderAtLayers(t *testing.T) {
	// Verify that Controller correctly identifies which layers the local
	// operator is a leader for.
	committee := []spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7}
	pubShares := make(map[tbftcore.OperatorID][]byte, 7)
	for _, op := range committee {
		pubShares[tbftcore.OperatorID(op)] = []byte{byte(op)}
	}

	// At slot 100: layer 0 leader = (100+0) mod 7 = 2 → op 3
	// layer 1 leader = (100+1) mod 7 = 3 → op 4
	// layer 2 leader = (100+2) mod 7 = 4 → op 5
	// So op 3 leads layer 0, op 4 leads layer 1, op 5 leads layer 2,
	// and ops 1, 2, 6, 7 lead no layer.

	for _, op := range committee {
		opID := op
		c, err := NewController(ControllerOptions{
			OperatorID:    opID,
			Committee:     committee,
			ClusterID:     [32]byte{0xAB},
			ClusterPubKey: []byte("clusterpk"),
			PubKeyShares:  pubShares,
			Signer:        tbftcore.NewStubSigner(5, []byte{byte(opID)}),
			IBE:           tbftcore.NewStubIBE(5),
		})
		require.NoError(t, err)

		r, err := c.StartNewInstance(phase0.Slot(100))
		require.NoError(t, err)

		switch op {
		case 3:
			require.Equal(t, []int{0}, r.LeaderAtLayers, "op %d should lead layer 0", op)
		case 4:
			require.Equal(t, []int{1}, r.LeaderAtLayers, "op %d should lead layer 1", op)
		case 5:
			require.Equal(t, []int{2}, r.LeaderAtLayers, "op %d should lead layer 2", op)
		default:
			require.Empty(t, r.LeaderAtLayers, "op %d should not lead any layer", op)
		}
	}
}

func TestController_NoSuchInstanceErrors(t *testing.T) {
	c := newStubController(t, 7)
	_, err := c.Resolve(phase0.Slot(99))
	require.ErrorContains(t, err, "no active instance")

	err = c.ObserveCandidate(phase0.Slot(99), 0, []byte("v"))
	require.ErrorContains(t, err, "no active instance")

	_, err = c.BuildOwnOnion(phase0.Slot(99))
	require.ErrorContains(t, err, "no active instance")
}

func TestController_RouteOnionByHeight(t *testing.T) {
	// Onion's Height field selects which instance receives it.
	c := newStubController(t, 7)
	_, err := c.StartNewInstance(phase0.Slot(100))
	require.NoError(t, err)
	_, err = c.StartNewInstance(phase0.Slot(101))
	require.NoError(t, err)

	// Onion for slot 102 (no instance) → error.
	o := &tbftcore.Onion{
		OperatorID: 1,
		Height:     102,
		Layers:     make([]tbftcore.EncryptedLayer, 3),
	}
	err = c.ProcessOnion(o)
	require.ErrorContains(t, err, "no active instance for slot 102")

	// Onion for slot 100 (instance exists) → OK.
	o.Height = 100
	require.NoError(t, c.ProcessOnion(o))
}

func TestController_NilMessageRejected(t *testing.T) {
	c := newStubController(t, 7)
	require.Error(t, c.ProcessOnion(nil))
	require.Error(t, c.ProcessNonReceipt(nil))
	require.Error(t, c.ProcessCandidate(nil))
}

func TestController_ProcessCandidate_RoutesByHeight(t *testing.T) {
	c := newStubController(t, 7)
	_, err := c.StartNewInstance(phase0.Slot(100))
	require.NoError(t, err)

	// Candidate for slot 200 (no instance) → error.
	cb := &tbftcore.CandidateBroadcast{
		OperatorID: 1,
		Height:     200,
		Layer:      0,
		Value:      []byte("v"),
	}
	require.ErrorContains(t, c.ProcessCandidate(cb), "no active instance for slot 200")

	// Candidate for slot 100 (instance exists) → OK.
	cb.Height = 100
	require.NoError(t, c.ProcessCandidate(cb))
}

func TestController_MultipleConcurrentInstances(t *testing.T) {
	// Distinct slots can have concurrent instances.
	c := newStubController(t, 7)
	for s := phase0.Slot(100); s <= 105; s++ {
		_, err := c.StartNewInstance(s)
		require.NoError(t, err)
	}
	require.Len(t, c.ActiveSlots(), 6)
	require.Equal(t,
		[]phase0.Slot{100, 101, 102, 103, 104, 105},
		c.ActiveSlots())
}

// ---- multi-controller end-to-end smoke test ----------------------------

// TestController_MultiCluster_HealthyEndToEnd spins up `n` Controllers
// (one per operator), each owning its own Instance for the same slot,
// gossips messages between them, and verifies they all converge on the
// same reconstructed signature.
//
// This is the integration smoke test for the adapter layer — it doesn't
// duplicate the protocol-level tests in [protocol/v2/tbft] since those
// already cover the failure modes. It just confirms the Controller's
// routing + delegation works end-to-end with real BLS + SignerGatedIBE.
func TestController_MultiCluster_HealthyEndToEnd(t *testing.T) {
	threshold.Init()
	const n = 7
	f := (n - 1) / 3
	q := 2*f + 1

	// Generate threshold-split BLS keypair.
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

	// Build one Controller per operator, each with its own share-bound
	// BLSSigner. Aggregation/verification are share-independent so any of
	// the controllers' signers can do them.
	controllers := make(map[spectypes.OperatorID]*Controller, n)
	for _, op := range committee {
		// `shares` is keyed by uint64; spectypes.OperatorID has the same
		// underlying type but Go requires the explicit cross-named-type
		// conversion at the index expression.
		opSigner := blsbackend.New(shares[uint64(op)].Serialize()) //nolint:unconvert

		c, err := NewController(ControllerOptions{
			OperatorID:    op,
			Committee:     committee,
			ClusterID:     [32]byte{0xCC, 0xCC},
			ClusterPubKey: masterPub,
			PubKeyShares:  pubShares,
			Signer:        opSigner,
			IBE:           ibe,
		})
		require.NoError(t, err)
		controllers[op] = c
	}

	const slot = phase0.Slot(42)

	// Phase 0: every controller starts an instance.
	K := 0
	for _, op := range committee {
		r, err := controllers[op].StartNewInstance(slot)
		require.NoError(t, err)
		if K == 0 {
			K = r.Config.K()
		}
	}

	// Phase 1: each operator (regardless of leader role) "sees" the
	// canonical candidate at every layer. In the real runner the layer
	// leaders fetch and broadcast, but for this smoke test we just
	// pre-populate every operator's view.
	candidates := make(map[int][]byte, K)
	for k := 0; k < K; k++ {
		candidates[k] = []byte(fmt.Sprintf("layer-%d-block-%d", k, slot))
		for _, op := range committee {
			require.NoError(t, controllers[op].ObserveCandidate(slot, k, candidates[k]))
		}
	}

	// Phase 2: each operator builds their onion + non-receipts, then
	// gossips them to all other operators.
	type prod struct {
		onion       *tbftcore.Onion
		nonReceipts []*tbftcore.NonReceiptAttestation
	}
	produces := make(map[spectypes.OperatorID]*prod, n)

	for _, op := range committee {
		o, err := controllers[op].BuildOwnOnion(slot)
		require.NoError(t, err)
		nrs, err := controllers[op].BuildOwnNonReceipts(slot)
		require.NoError(t, err)
		produces[op] = &prod{onion: o, nonReceipts: nrs}
	}

	for _, recv := range committee {
		for _, sender := range committee {
			require.NoError(t, controllers[recv].ProcessOnion(produces[sender].onion))
			for _, nr := range produces[sender].nonReceipts {
				require.NoError(t, controllers[recv].ProcessNonReceipt(nr))
			}
		}
	}

	// Phase 3: each operator resolves; all should produce the SAME output.
	var ref *tbftcore.Output
	for _, op := range committee {
		out, err := controllers[op].Resolve(slot)
		require.NoError(t, err, "op %d should produce output", op)
		require.NotNil(t, out)
		require.Equal(t, 0, out.Layer, "healthy case decides at layer 0")
		require.True(t, bytes.Equal(out.Value, candidates[0]))

		if ref == nil {
			ref = out
			continue
		}
		require.True(t, bytes.Equal(ref.Signature, out.Signature),
			"all controllers must derive the same reconstructed signature")
	}

	// Reconstructed signature must verify under the master pubkey.
	require.True(t, verifier.VerifyAggregate(masterPub, ref.Value, ref.Signature))

	// Lifecycle: end the instance on every controller; subsequent operations error.
	for _, op := range committee {
		controllers[op].EndInstance(slot)
		_, err := controllers[op].Resolve(slot)
		require.Error(t, err, "resolve after EndInstance must fail")
	}
}

// ---- helpers -------------------------------------------------------------

// newStubController builds a Controller backed by stub primitives for
// API-surface tests that don't need real cryptography.
func newStubController(t *testing.T, n int) *Controller {
	t.Helper()
	committee := make([]spectypes.OperatorID, n)
	pubShares := make(map[tbftcore.OperatorID][]byte, n)
	for i := 0; i < n; i++ {
		committee[i] = spectypes.OperatorID(i + 1)
		pubShares[tbftcore.OperatorID(i+1)] = []byte{byte(i + 1)}
	}
	q := 2*((n-1)/3) + 1

	c, err := NewController(ControllerOptions{
		OperatorID:    1,
		Committee:     committee,
		ClusterID:     [32]byte{},
		ClusterPubKey: []byte("clusterpk"),
		PubKeyShares:  pubShares,
		Signer:        tbftcore.NewStubSigner(q, []byte{1}),
		IBE:           tbftcore.NewStubIBE(q),
	})
	require.NoError(t, err)
	return c
}
