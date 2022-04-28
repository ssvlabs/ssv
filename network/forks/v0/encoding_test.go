package v0

import (
	"bytes"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestForkV0_Encoding(t *testing.T) {
	msg := &network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Type:   proto.RoundState_Commit,
				Round:  1,
				Lambda: []byte("YTAyZjNhNGQ5ZDg2NTZkMmI0NDI3NzVjM2JlNDliMzU1ZDc0MDU5OGNiYjM5NzAyNmZhNzRkYzUxZTFlN2FhOGVmZTJjZjk3ZTQ0ZjZmYWQxMTE4NWY2M2I2MTUxY2Q0X0FUVEVTVEVS"),
				Value:  []byte("data"),
			},
			Signature: []byte("sig"),
			SignerIds: []uint64{1, 2, 3, 4},
		},
		Type: network.NetworkMsg_DecidedType,
	}
	f := &ForkV0{}

	v1Msg, err := ToV1Message(msg)
	require.NoError(t, err)

	b, err := f.EncodeNetworkMsg(v1Msg)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)

	res, err := f.DecodeNetworkMsg(b)
	require.NoError(t, err)
	require.Equal(t, v1Msg.MsgType, res.MsgType)
	require.True(t, bytes.Equal(v1Msg.Data, res.Data))
}
