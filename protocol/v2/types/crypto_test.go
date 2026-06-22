package types

import (
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestVerifyBeaconPartialSignature(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))
	require.NoError(t, bls.SetETHmode(bls.EthModeDraft07))

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()
	pk := sk.GetPublicKey().Serialize()

	const signer = spectypes.OperatorID(3)
	committee := []*spectypes.ShareMember{{Signer: signer, SharePubKey: pk}}

	var root [32]byte
	copy(root[:], "the-root-we-expect-to-be-signed.")
	goodSig := sk.SignByte(root[:]).Serialize()

	// a correct signature over the expected root, by a committee member, verifies
	require.NoError(t, VerifyBeaconPartialSignature(signer, goodSig, root, committee))

	// a signature over a different root (the wrong-domain / misconfigured-signer case) is rejected
	var otherRoot [32]byte
	copy(otherRoot[:], "a-different-root-the-wrong-domain")
	require.Error(t, VerifyBeaconPartialSignature(signer, sk.SignByte(otherRoot[:]).Serialize(), root, committee))

	// a signer not in the committee is rejected
	require.Error(t, VerifyBeaconPartialSignature(spectypes.OperatorID(99), goodSig, root, committee))
}
