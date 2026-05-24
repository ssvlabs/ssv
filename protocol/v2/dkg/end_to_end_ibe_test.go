package dkg

import (
	"context"
	"testing"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// TestEndToEnd_DKGOutput_TLockIBE — the end-to-end capstone. Proves that
// the OBFT-IBE Option-B pipeline works end-to-end with a DKG-derived IBE
// keypair:
//
//  1. Run Pedersen DKG via the Coordinator at threshold qEnc = f+1.
//  2. Encrypt a plaintext under TLockIBE using the cluster IBE pubkey
//     (Commits[0]) as the trust anchor.
//  3. Each operator signs the encryption tag with KyberSigner using
//     their DKG-derived IBE share.
//  4. Aggregate exactly qEnc partials → IBE decryption witness.
//  5. TLockIBE.Decrypt recovers the plaintext.
//
// Plus the threshold-separation guarantee: aggregating only `qEnc - 1 = f`
// partials does NOT decrypt — confirms that the DKG-output polynomial is
// genuinely degree f and the threshold count is load-bearing.
func TestEndToEnd_DKGOutput_TLockIBE(t *testing.T) {
	committee := []uint64{1, 2, 3, 4, 5, 6, 7}
	f := (len(committee) - 1) / 3
	qEnc := f + 1 // 3

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	results, errs := runCluster(t, ctx, committee, qEnc, cidFor(0xc2), 0, nil)
	require.Empty(t, errs)
	require.Len(t, results, len(committee))

	// Cluster IBE pubkey = Commits[0] across all operators.
	clusterIBEPubKey, err := results[committee[0]].Commits[0].MarshalBinary()
	require.NoError(t, err)

	tag := []byte("obft-no-quorum-tag/v1")
	plaintext := []byte("opaque payload — could be any partial-sig bytes the protocol IBE-wraps")

	ibe := blsbackend.NewTLockIBE()
	ciphertext, err := ibe.Encrypt(clusterIBEPubKey, tag, plaintext)
	require.NoError(t, err)
	require.NotEqual(t, plaintext, ciphertext)

	// Each operator signs the tag with KyberSigner using their DKG share.
	signer := blsbackend.NewKyberSigner(nil)
	partials := make(map[obftcore.OperatorID]obftcore.Signature, len(committee))
	for _, opID := range committee {
		shareBytes, err := results[opID].Share.V.MarshalBinary()
		require.NoError(t, err)
		opSigner := blsbackend.NewKyberSigner(shareBytes)
		sig, err := opSigner.SignPartial(tag)
		require.NoError(t, err)
		partials[obftcore.OperatorID(opID)] = sig
	}

	// Threshold-separation proof: exactly qEnc partials decrypts.
	subset := make(map[obftcore.OperatorID]obftcore.Signature, qEnc)
	for _, opID := range committee[:qEnc] {
		subset[obftcore.OperatorID(opID)] = partials[obftcore.OperatorID(opID)]
	}
	decryptionKey, err := signer.AggregatePartials(subset)
	require.NoError(t, err)

	recovered, err := ibe.Decrypt(ciphertext, decryptionKey)
	require.NoError(t, err)
	require.Equal(t, plaintext, recovered,
		"TLockIBE decryption with qEnc=%d DKG-share partials must recover the plaintext", qEnc)

	// Cross-subset consistency: a different qEnc-sized subset produces
	// the same decryption key (Lagrange invariance) and therefore
	// decrypts the same plaintext.
	subset2 := make(map[obftcore.OperatorID]obftcore.Signature, qEnc)
	for _, opID := range committee[len(committee)-qEnc:] {
		subset2[obftcore.OperatorID(opID)] = partials[obftcore.OperatorID(opID)]
	}
	decryptionKey2, err := signer.AggregatePartials(subset2)
	require.NoError(t, err)
	require.Equal(t, decryptionKey, decryptionKey2)

	// Below-threshold: f = qEnc-1 partials must NOT decrypt. Lagrange
	// over fewer points produces a different scalar → different (wrong)
	// decryption key → IBE decryption fails.
	belowSubset := make(map[obftcore.OperatorID]obftcore.Signature, f)
	for _, opID := range committee[:f] {
		belowSubset[obftcore.OperatorID(opID)] = partials[obftcore.OperatorID(opID)]
	}
	wrongKey, err := signer.AggregatePartials(belowSubset)
	require.NoError(t, err) // aggregate succeeds; the resulting key is just wrong
	_, err = ibe.Decrypt(ciphertext, wrongKey)
	require.Error(t, err,
		"decryption with below-threshold (f=%d) partials must fail", f)

	// Keypair-distinctness proof: a *separate* validator threshold key
	// (the existing SSV ceremony — herumi-format split via threshold.Create
	// at qV = 2f+1) yields shares that are not related to the DKG's
	// IBE polynomial. Aggregating any number of validator-share partials
	// on the same tag produces a sig that does NOT decrypt the IBE
	// ciphertext — confirming Option B's IBE keypair is genuinely
	// independent of the validator keypair (not the DST-trick reuse).
	threshold.Init()
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	qV := 2*f + 1
	validatorShares, err := threshold.Create(master.Serialize(), uint64(qV), uint64(len(committee)))
	require.NoError(t, err)

	validatorPartials := make(map[obftcore.OperatorID]obftcore.Signature, len(committee))
	for opID, sk := range validatorShares {
		opSigner := blsbackend.NewKyberSigner(sk.Serialize())
		sig, err := opSigner.SignPartial(tag)
		require.NoError(t, err)
		validatorPartials[obftcore.OperatorID(opID)] = sig
	}
	validatorAggregate, err := signer.AggregatePartials(validatorPartials)
	require.NoError(t, err)
	_, err = ibe.Decrypt(ciphertext, validatorAggregate)
	require.Error(t, err,
		"decryption with validator-share partials must fail under Option B (separate IBE keypair)")
}
