package ssv_test

import (
	"encoding/hex"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/protocol/v1/message"

	//"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValidatorID_MessageIDBelongs(t *testing.T) {
	t.Run("msg id belongs", func(t *testing.T) {
		msgID := []byte{1, 2, 3, 4, 1, 0, 0, 0}
		valID := types.ValidatorPK{1, 2, 3, 4}
		require.True(t, valID.MessageIDBelongs(msgID))
	})

	t.Run("msg id doesn't belong", func(t *testing.T) {
		msgID := []byte{1, 2, 3, 4, 1, 0, 0, 0}
		valID := types.ValidatorPK{1, 2, 3, 3}
		require.False(t, valID.MessageIDBelongs(msgID))
	})

	t.Run("msg id doesn't belong", func(t *testing.T) {
		msgID := []byte{1, 2, 3, 4, 1, 0, 0, 0}
		valID := types.ValidatorPK{1, 2, 3, 4, 4}
		require.False(t, valID.MessageIDBelongs(msgID))
	})
}

func TestConsensusData_Marshaling(t *testing.T) {
	expected, _ := hex.DecodeString("7b2244757479223a7b2254797065223a312c225075624b6579223a5b3134382c3134332c3138302c36392c3133302c3230362c33372c35312c3131312c3231392c32332c31382c34362c3137322c3130302c3235342c39302c32362c3235322c35372c32332c37362c3233332c34352c39362c31392c3139302c3230322c3139332c32322c3131382c3130392c3139372c3136372c3132302c3230302c3132382c3232312c37312c3232322c3132352c3235352c3234362c3136302c3234382c3130372c3136342c34345d2c22536c6f74223a31322c2256616c696461746f72496e646578223a312c22436f6d6d6974746565496e646578223a332c22436f6d6d69747465654c656e677468223a3132382c22436f6d6d6974746565734174536c6f74223a33362c2256616c696461746f72436f6d6d6974746565496e646578223a31317d2c224174746573746174696f6e44617461223a7b22736c6f74223a223132222c22696e646578223a2233222c22626561636f6e5f626c6f636b5f726f6f74223a22307830313032303330343035303630373038303930613031303230333034303530363037303830393061303130323033303430353036303730383039306130313032222c22736f75726365223a7b2265706f6368223a2230222c22726f6f74223a22307830313032303330343035303630373038303930613031303230333034303530363037303830393061303130323033303430353036303730383039306130313032227d2c22746172676574223a7b2265706f6368223a2231222c22726f6f74223a22307830313032303330343035303630373038303930613031303230333034303530363037303830393061303130323033303430353036303730383039306130313032227d7d2c22426c6f636b44617461223a6e756c6c7d")

	t.Run("attestation data", func(t *testing.T) {
		c := testingutils.TestAttesterConsensusData
		byts, err := c.Encode()
		require.NoError(t, err)
		require.EqualValues(t, expected, byts)
	})

	t.Run("marshal with attestation data", func(t *testing.T) {
		c := &types.ConsensusData{}
		require.NoError(t, c.Decode(expected))
		require.EqualValues(t, message.RoleTypeAttester, c.Duty.Type)
		require.EqualValues(t, testingutils.TestingValidatorPubKey, c.Duty.PubKey)
		require.EqualValues(t, spec.Root{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2}, c.AttestationData.BeaconBlockRoot)
	})
}
