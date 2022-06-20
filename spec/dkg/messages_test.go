package dkg

import (
	"encoding/hex"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"testing"
)

var TestingValidatorPubKey = func() spec.BLSPubKey {
	// sk - 3515c7d08e5affd729e9579f7588d30f2342ee6f6a9334acf006345262162c6f
	pk, _ := hex.DecodeString("8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0")
	blsPK := spec.BLSPubKey{}
	copy(blsPK[:], pk)
	return blsPK
}()

func TestDKGOutput_GetRoot(t *testing.T) {
	dkg := &Output{
		WithdrawalCredentials: make([]byte, 100),
		DKGSetSize:            4,
		ValidatorPubKey:       TestingValidatorPubKey[:],
		EncryptedShare:        make([]byte, 50),
	}

	r, err := dkg.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470", hex.EncodeToString(r))
}
