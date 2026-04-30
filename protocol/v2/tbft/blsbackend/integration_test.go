package blsbackend_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/blsbackend"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// Integration tests: drive the full TBFT pipeline (Phase 1 → Phase 2 →
// Phase 3) with the herumi/bls-backed Signer, using real threshold-split
// keypairs.
//
// `TestProtocol_Healthy_n7_BLSBackend` exercises the layer-0 happy path
// (no IBE decryption invoked). `TestProtocol_TopLeaderSilent_n7_BLSBackend`
// exercises layer fallthrough using SignerGatedIBE — real BLS access-gate
// verification, no cryptographic confidentiality. Production deployment
// will swap SignerGatedIBE for a real cryptographic IBE (drand/tlock or
// equivalent) without touching protocol code.
func TestProtocol_Healthy_n7_BLSBackend(t *testing.T) {
	threshold.Init()

	const n = 7
	f := (n - 1) / 3
	q := 2*f + 1
	K := 3 // max(3, f+1) = max(3, 3) = 3

	// 1. Generate a real threshold-split BLS keypair.
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)

	pubKeyShares := make(map[tbft.OperatorID][]byte, n)
	for id, sk := range shares {
		pubKeyShares[tbft.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}

	signer := blsbackend.New()
	ibe := tbft.NewStubIBE(q) // unused in this test (no fallthrough)

	// 2. Build the cluster Config.
	operators := make([]tbft.OperatorID, n)
	for i := 0; i < n; i++ {
		operators[i] = tbft.OperatorID(i + 1)
	}
	layers := make([]tbft.LayerSpec, K)
	for i := 0; i < K; i++ {
		layers[i] = tbft.LayerSpec{Leader: tbft.OperatorID(i + 1), FetchAt: 1_000_000_000}
	}
	cfg := &tbft.Config{
		Height:    42,
		Layers:    layers,
		Deadline:  3_000_000_000,
		ClusterID: [32]byte{0xCA, 0xFE},
		Operators: operators,
		F:         f,
	}
	require.NoError(t, cfg.Validate())

	// 3. Cluster-wide canonical candidates per layer (all operators see them).
	candidates := make([]tbft.Value, K)
	for k := 0; k < K; k++ {
		candidates[k] = tbft.Value(fmt.Sprintf("BLS-real-cluster-block-layer-%d", k))
	}

	// 4. Each operator builds an Instance + onion + (no non-receipts since
	//    everyone sees everything).
	type produced struct {
		onion *tbft.Onion
	}
	produces := make(map[tbft.OperatorID]*produced)
	instances := make(map[tbft.OperatorID]*tbft.Instance)

	for _, op := range operators {
		share := shares[uint64(op)].Serialize()
		inst, err := tbft.NewInstance(cfg, signer, ibe, masterPub, pubKeyShares)
		require.NoError(t, err)
		instances[op] = inst

		for k := 0; k < K; k++ {
			require.NoError(t, inst.ObserveCandidate(k, candidates[k]))
		}
		onion, err := inst.BuildOwnOnion(op, share)
		require.NoError(t, err)
		produces[op] = &produced{onion: onion}
	}

	// 5. Gossip: every operator observes every other operator's onion.
	for _, recv := range operators {
		for _, sender := range operators {
			require.NoError(t, instances[recv].ObserveOnion(produces[sender].onion))
		}
	}

	// 6. Resolve. All operators should produce the SAME signed output at
	//    layer 0.
	var ref *tbft.Output
	for _, op := range operators {
		out, err := instances[op].Resolve()
		require.NoError(t, err, "op %d should produce an output", op)
		require.NotNil(t, out)
		require.Equal(t, 0, out.Layer, "healthy case decides at layer 0")
		require.True(t, bytes.Equal(out.Value, candidates[0]),
			"decided value should be layer 0's candidate")

		if ref == nil {
			ref = out
			continue
		}
		require.True(t, bytes.Equal(ref.Signature, out.Signature),
			"all operators must derive the same reconstructed signature")
	}

	// 7. The reconstructed signature MUST verify against the master pubkey
	//    for the decided value — this is the property SSV's beacon-side
	//    submission relies on.
	require.True(t, signer.VerifyAggregate(masterPub, ref.Value, ref.Signature),
		"reconstructed cluster signature must verify under the master pubkey")

	// And it must equal what the master key would sign directly (BLS
	// determinism).
	masterDirect := master.SignByte(ref.Value).Serialize()
	require.True(t, bytes.Equal(masterDirect, ref.Signature),
		"reconstructed signature should equal the master's direct signature")
}

// TestProtocol_TopLeaderSilent_n7_BLSBackend exercises the layer-fallthrough
// path with REAL BLS cryptography for the access gate (via SignerGatedIBE).
//
// Scenario: top leader (layer 0) silent. All operators have layer-1 and
// layer-2 candidates. Each operator emits a non-receipt for layer 0; the
// non-receipts aggregate into a real BLS signature on the layer-0
// no-quorum tag, which serves as the IBE decryption key for layer 1.
// SignerGatedIBE verifies the key against the master pubkey and only
// then exposes the layer-1 partial sigs. Layer 1 then reaches positive
// quorum and the cluster outputs a reconstructed signature on layer 1's
// candidate.
func TestProtocol_TopLeaderSilent_n7_BLSBackend(t *testing.T) {
	threshold.Init()

	const n = 7
	f := (n - 1) / 3
	q := 2*f + 1
	K := 3

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)

	pubKeyShares := make(map[tbft.OperatorID][]byte, n)
	for id, sk := range shares {
		pubKeyShares[tbft.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}

	signer := blsbackend.New()
	ibe := blsbackend.NewSignerGatedIBE(signer, masterPub) // real BLS access gate

	operators := make([]tbft.OperatorID, n)
	for i := 0; i < n; i++ {
		operators[i] = tbft.OperatorID(i + 1)
	}
	layers := make([]tbft.LayerSpec, K)
	for i := 0; i < K; i++ {
		layers[i] = tbft.LayerSpec{Leader: tbft.OperatorID(i + 1), FetchAt: 1_000_000_000}
	}
	cfg := &tbft.Config{
		Height:    100,
		Layers:    layers,
		Deadline:  3_000_000_000,
		ClusterID: [32]byte{0xDE, 0xAD, 0xBE, 0xEF},
		Operators: operators,
		F:         f,
	}
	require.NoError(t, cfg.Validate())

	// Candidates exist for layers 1 and 2 only — layer 0 leader is silent.
	candidates := map[int]tbft.Value{
		1: tbft.Value("layer-1-fallback-block"),
		2: tbft.Value("layer-2-deepest-fallback-block"),
	}

	type produced struct {
		onion       *tbft.Onion
		nonReceipts []*tbft.NonReceiptAttestation
	}
	produces := make(map[tbft.OperatorID]*produced)
	instances := make(map[tbft.OperatorID]*tbft.Instance)

	for _, op := range operators {
		share := shares[uint64(op)].Serialize()
		inst, err := tbft.NewInstance(cfg, signer, ibe, masterPub, pubKeyShares)
		require.NoError(t, err)
		instances[op] = inst

		// Each operator only sees layers 1 and 2.
		for k, v := range candidates {
			require.NoError(t, inst.ObserveCandidate(k, v))
		}

		onion, err := inst.BuildOwnOnion(op, share)
		require.NoError(t, err)
		nrs, err := inst.BuildOwnNonReceipts(op, share)
		require.NoError(t, err)
		produces[op] = &produced{onion: onion, nonReceipts: nrs}
	}

	// Gossip everything to everyone.
	for _, recv := range operators {
		for _, sender := range operators {
			require.NoError(t, instances[recv].ObserveOnion(produces[sender].onion))
			for _, nr := range produces[sender].nonReceipts {
				require.NoError(t, instances[recv].ObserveNonReceipt(nr))
			}
		}
	}

	// Each operator resolves; all should converge on the same layer-1 output.
	var ref *tbft.Output
	for _, op := range operators {
		out, err := instances[op].Resolve()
		require.NoError(t, err, "op %d should produce an output (layer-1 fallthrough)", op)
		require.NotNil(t, out)
		require.Equal(t, 1, out.Layer, "layer 0 silent → fallthrough to layer 1")
		require.True(t, bytes.Equal(out.Value, candidates[1]))

		if ref == nil {
			ref = out
			continue
		}
		require.True(t, bytes.Equal(ref.Signature, out.Signature),
			"all operators must derive the same reconstructed signature for layer 1")
	}

	// The reconstructed signature must verify against the master pubkey
	// for layer 1's value.
	require.True(t, signer.VerifyAggregate(masterPub, ref.Value, ref.Signature),
		"reconstructed layer-1 signature must verify under the master pubkey")
	require.True(t, bytes.Equal(master.SignByte(ref.Value).Serialize(), ref.Signature),
		"reconstructed signature should match what the master would sign")
}
