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

// TestEndToEndRealIBE_LayerFallthrough — the integration capstone.
//
// Exercises the full TBFT protocol with REAL cryptographic IBE
// (TLockIBE) using the DST-trick: operators carry their existing
// herumi-format BLS shares; a BLSSigner signs candidate values using
// the Eth2 DST; a KyberSigner signs IBE no-quorum tags using kyber's
// drand-DST; the kyber aggregate is a valid IBE decryption key for
// TLockIBE-encrypted ciphertexts. NO separate IBE DKG.
//
// Scenario: top leader (layer 0) silent. All operators emit non-receipts
// for layer 0; the kyber-aggregated non-receipt sig decrypts the layer-1
// onion contributions; quorum reached at layer 1; reconstructed
// validator signature on layer 1's value is the herumi-format threshold
// sig (Eth2-compatible).
//
// If this test passes, the TBFT protocol is functionally complete with
// real IBE: an SSV proposer-duty integration is purely runner-side
// wiring, no cryptographic open questions remain.
func TestEndToEndRealIBE_LayerFallthrough(t *testing.T) {
	threshold.Init()
	const n = 7
	f := (n - 1) / 3
	q := 2*f + 1
	K := 3

	// Generate the validator's threshold key (existing SSV ceremony — no
	// changes to share generation).
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()
	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)

	pubKeyShares := make(map[tbft.OperatorID][]byte, n)
	for id, sk := range shares {
		pubKeyShares[tbft.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}

	// Two signers: BLSSigner (value-signing, Eth2 DST) + KyberSigner
	// (tag-signing, drand DST). BOTH consume the same operator share.
	valueSigner := blsbackend.New()
	tagSigner := blsbackend.NewKyberSigner()
	ibeImpl := blsbackend.NewTLockIBE()

	// Build cluster Config.
	operators := make([]tbft.OperatorID, n)
	for i := 0; i < n; i++ {
		operators[i] = tbft.OperatorID(i + 1)
	}
	layers := make([]tbft.LayerSpec, K)
	for i := 0; i < K; i++ {
		layers[i] = tbft.LayerSpec{Leader: tbft.OperatorID(i + 1), FetchAt: 1_000_000_000}
	}
	cfg := &tbft.Config{
		Height:    77,
		Layers:    layers,
		Deadline:  3_000_000_000,
		ClusterID: [32]byte{0xBE, 0xEF},
		Operators: operators,
		F:         f,
	}
	require.NoError(t, cfg.Validate())

	// Layer-0 leader is "silent": no candidate at layer 0 for anyone.
	// Layer-1 and layer-2 candidates are visible to all.
	candidates := map[int]tbft.Value{
		1: tbft.Value(fmt.Sprintf("layer-1-block-slot-%d", cfg.Height)),
		2: tbft.Value(fmt.Sprintf("layer-2-deepest-fallback-block-slot-%d", cfg.Height)),
	}

	type produced struct {
		onion       *tbft.Onion
		nonReceipts []*tbft.NonReceiptAttestation
	}
	produces := make(map[tbft.OperatorID]*produced)
	instances := make(map[tbft.OperatorID]*tbft.Instance)

	// Each operator builds an Instance with BOTH signers.
	for _, op := range operators {
		share := shares[uint64(op)].Serialize()
		inst, err := tbft.NewInstanceWithTagSigner(cfg, valueSigner, tagSigner, ibeImpl, masterPub, pubKeyShares)
		require.NoError(t, err)
		instances[op] = inst

		for k, v := range candidates {
			require.NoError(t, inst.ObserveCandidate(k, v))
		}

		onion, err := inst.BuildOwnOnion(op, share)
		require.NoError(t, err)
		nrs, err := inst.BuildOwnNonReceipts(op, share)
		require.NoError(t, err)
		produces[op] = &produced{onion: onion, nonReceipts: nrs}
	}

	// Gossip: all operators observe all peers' messages.
	for _, recv := range operators {
		for _, sender := range operators {
			require.NoError(t, instances[recv].ObserveOnion(produces[sender].onion))
			for _, nr := range produces[sender].nonReceipts {
				require.NoError(t, instances[recv].ObserveNonReceipt(nr))
			}
		}
	}

	// Resolve. All should produce the SAME layer-1 output.
	var ref *tbft.Output
	for _, op := range operators {
		out, err := instances[op].Resolve()
		require.NoError(t, err, "op %d Resolve", op)
		require.NotNil(t, out)
		require.Equal(t, 1, out.Layer, "layer 0 silent → fallthrough to layer 1")
		require.True(t, bytes.Equal(out.Value, candidates[1]))

		if ref == nil {
			ref = out
			continue
		}
		require.True(t, bytes.Equal(ref.Signature, out.Signature),
			"all operators must derive the same reconstructed validator signature")
	}

	// The reconstructed signature must verify under the master pubkey
	// for the decided value — and should equal what the master would
	// sign directly (BLS determinism). This is the Eth2-compatible
	// signature SSV's beacon submission expects.
	require.True(t, valueSigner.VerifyAggregate(masterPub, ref.Value, ref.Signature),
		"reconstructed signature must verify under master pubkey (Eth2 BLS)")
	masterDirect := master.SignByte(ref.Value).Serialize()
	require.True(t, bytes.Equal(masterDirect, ref.Signature),
		"reconstructed signature should equal what master would sign directly")
}
