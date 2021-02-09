package threshold

import (
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
)

func TestSplitAndReconstruct(t *testing.T) {
	Init()

	// generate random secret and split
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()
	shares, err := Create(sk.Serialize(), 4)
	require.NoError(t, err)

	// partial sigs
	m := make(map[uint64][]byte)
	for i, s := range shares {
		partialSig := s.SignByte([]byte("hello"))
		m[i] = partialSig.Serialize()
	}

	// reconstruct
	sig, err := ReconstructSignatures(m)
	originalSig := bls.Sign{}
	require.NoError(t, originalSig.Deserialize(sig))
	require.True(t, originalSig.VerifyByte(sk.GetPublicKey(), []byte("hello")))
}
