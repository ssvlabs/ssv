package blsbackend_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// Smoke test: drive a healthy-path OBFT instance with the herumi/bls-backed
// Signer, real threshold-split keypairs, and SignerGatedIBE. Verifies the
// blsbackend implementations satisfy obft.Signer / obft.ThresholdIBE
// contracts as the protocol exercises them.
//
// Per-primitive correctness lives in the dedicated *_test.go files for
// signer / kyber_signer / signer_gated_ibe / tlock_ibe / kyber_conversion;
// this test exercises the integration of the primitives with the protocol's
// state machine on a realistic-shaped slot.
func TestProtocol_Healthy_n4_K4_RealBLS(t *testing.T) {
	threshold.Init()

	const n = 4
	const f = 1
	const q = 2*f + 1
	const K = 4

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)

	pubKeyShares := make(map[obft.OperatorID][]byte, n)
	for id, sk := range shares {
		pubKeyShares[obft.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}

	operators := make([]obft.OperatorID, n)
	for i := 0; i < n; i++ {
		operators[i] = obft.OperatorID(i + 1)
	}
	layers := make([]obft.LayerSpec, K)
	for k := 0; k < K; k++ {
		layers[k] = obft.LayerSpec{
			Leader:  operators[k],
			FetchAt: 1100*time.Millisecond - time.Duration(k)*50*time.Millisecond,
		}
	}
	cfg := &obft.Config{
		Height:    42,
		ClusterID: [32]byte{0xCA, 0xFE},
		Operators: operators,
		F:         f,
		Layers:    layers,
		TCommit:   1500 * time.Millisecond,
		Delta2:    300 * time.Millisecond,
		Delta3:    250 * time.Millisecond,
		BTT:       150 * time.Millisecond, // P99=100 + δ=50 fixture
	}
	require.NoError(t, cfg.Validate())

	// IBE under SignerGatedIBE — real BLS access-gate verification, no
	// cryptographic confidentiality. Sufficient for protocol-flow testing.
	verifier := blsbackend.New(nil)
	ibe := blsbackend.NewSignerGatedIBE(verifier, masterPub)

	candidates := make(map[int]obft.Value, K)
	for k := 0; k < K; k++ {
		candidates[k] = obft.Value(fmt.Sprintf("BLS-real-block-layer-%d", k))
	}

	// Build instances + drive the slot.
	instances := make(map[obft.OperatorID]*obft.Instance, n)
	for _, op := range operators {
		share := shares[uint64(op)].Serialize()
		signer := blsbackend.New(share)
		inst, err := obft.NewInstance(cfg, op, signer, signer, ibe, masterPub, pubKeyShares, nil, nil)
		require.NoError(t, err)
		instances[op] = inst
	}

	// Phase 1: each layer's leader builds + everyone observes + applies host validity.
	bundles := make(map[int]*obft.Phase1Bundle, K)
	for k := 0; k < K; k++ {
		leader := layers[k].Leader
		b, err := instances[leader].BuildPhase1Bundle(k, candidates[k])
		require.NoError(t, err)
		bundles[k] = b
		require.NoError(t, instances[leader].ApplyHostValidity(k, candidates[k], true))
	}
	for _, op := range operators {
		for k := 0; k < K; k++ {
			if op == layers[k].Leader {
				continue
			}
			require.NoError(t, instances[op].ObservePhase1Bundle(bundles[k], 1200*time.Millisecond))
			require.NoError(t, instances[op].ApplyHostValidity(k, candidates[k], true))
		}
	}

	// Phase 2: everyone builds + broadcasts a single Commit (combining σ
	// partials and NR partials per spec §Phase 2).
	commits := make(map[obft.OperatorID]*obft.Commit, n)
	for _, op := range operators {
		c, err := instances[op].BuildOwnCommit()
		require.NoError(t, err)
		commits[op] = c
	}
	for receiver, inst := range instances {
		for sender, c := range commits {
			if sender == receiver {
				continue
			}
			require.NoError(t, inst.ObserveCommit(c))
		}
	}

	// Phase 3: every operator should reach σ-quorum at L_0.
	var ref *obft.Output
	for _, op := range operators {
		out, err := instances[op].Resolve()
		require.NoError(t, err, "op %d should reach quorum", op)
		require.Equal(t, 0, out.Layer, "healthy path decides at L_0")
		require.True(t, bytes.Equal(out.Value, candidates[0]))
		if ref == nil {
			ref = out
			continue
		}
		require.True(t, bytes.Equal(ref.Signature, out.Signature),
			"all operators must reconstruct byte-identical full signature")
	}

	// And the reconstructed signature should verify against the cluster
	// pubkey — this is the load-bearing claim of threshold-BLS.
	require.True(t, verifier.VerifyAggregate(masterPub, candidates[0], ref.Signature),
		"reconstructed signature must verify against cluster master pubkey")
}
