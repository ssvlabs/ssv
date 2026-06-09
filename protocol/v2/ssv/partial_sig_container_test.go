package ssv

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// TestResolveDuplicateSignature covers the three outcomes when a second partial signature
// arrives for an already-seen (validator, signer, root): keep a still-valid existing one,
// replace an invalid existing one with a valid incoming one, or drop both when neither verifies.
func TestResolveDuplicateSignature(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))
	require.NoError(t, bls.SetETHmode(bls.EthModeDraft07))

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()
	pk := sk.GetPublicKey().Serialize()

	const signer = spectypes.OperatorID(3)
	const vIdx = phase0.ValidatorIndex(7)
	committee := []*spectypes.ShareMember{{Signer: signer, SharePubKey: pk}}

	var root [32]byte
	copy(root[:], "the-root-we-expect-to-be-signed.")
	var otherRoot [32]byte
	copy(otherRoot[:], "a-different-root-the-wrong-domain")

	validSig := sk.SignByte(root[:]).Serialize()        // verifies against root
	invalidSig := sk.SignByte(otherRoot[:]).Serialize() // valid BLS, but over the wrong root

	msg := func(sig spectypes.Signature) *spectypes.PartialSignatureMessage {
		return &spectypes.PartialSignatureMessage{
			PartialSignature: sig,
			SigningRoot:      root,
			Signer:           signer,
			ValidatorIndex:   vIdx,
		}
	}

	t.Run("keeps a valid existing signature", func(t *testing.T) {
		ps := NewPartialSigContainer(1)
		ps.AddSignature(msg(validSig))

		require.NoError(t, ps.ResolveDuplicateSignature(msg(invalidSig), committee))

		got, err := ps.GetSignature(vIdx, signer, root)
		require.NoError(t, err)
		require.NoError(t, types.VerifyBeaconPartialSignature(signer, got, root, committee))
	})

	t.Run("replaces an invalid existing signature with the valid one", func(t *testing.T) {
		ps := NewPartialSigContainer(1)
		ps.AddSignature(msg(invalidSig))

		require.NoError(t, ps.ResolveDuplicateSignature(msg(validSig), committee))

		got, err := ps.GetSignature(vIdx, signer, root)
		require.NoError(t, err)
		require.NoError(t, types.VerifyBeaconPartialSignature(signer, got, root, committee))
	})

	t.Run("drops the signature and errors when neither verifies", func(t *testing.T) {
		ps := NewPartialSigContainer(1)
		ps.AddSignature(msg(invalidSig))

		require.Error(t, ps.ResolveDuplicateSignature(msg(invalidSig), committee))

		_, err := ps.GetSignature(vIdx, signer, root)
		require.Error(t, err)
	})
}
