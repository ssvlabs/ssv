package v0

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestForkV0_ValidatorTopicID(t *testing.T) {
	threshold.Init()
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()

	pk := hex.EncodeToString(sk.GetPublicKey().Serialize())

	require.EqualValues(t, sk.GetPublicKey().SerializeToHexStr(), pk)
}
