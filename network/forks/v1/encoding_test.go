package v1

import (
	"bytes"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
	"testing"
)

// TODO: remove after fork
func TestForkV0_Encoding(t *testing.T) {
	msg := &network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Type:   proto.RoundState_Decided,
				Round:  1,
				Lambda: []byte("xxxxxxxx_ATTESTER"),
				Value:  []byte("data"),
			},
			Signature: []byte("sig"),
			SignerIds: []uint64{1, 2, 3, 4},
		},
		Type: network.NetworkMsg_DecidedType,
	}
	f := &ForkV1{}

	b, err := f.EncodeNetworkMsg(msg)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)

	res, err := f.DecodeNetworkMsg(b)
	require.NoError(t, err)
	require.Equal(t, msg.Type, res.(*network.Message).Type)
	require.Equal(t, msg.SignedMessage.SignerIds[0], res.(*network.Message).SignedMessage.SignerIds[0])
}

func TestForkV1_Encoding(t *testing.T) {
	msg := &message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      []byte("xxxxxxxxxxx_ATTESTER"),
		Data:    []byte("data"),
	}
	f := &ForkV1{}

	b, err := f.EncodeNetworkMsgV1(msg)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)

	res, err := f.DecodeNetworkMsgV1(b)
	require.NoError(t, err)
	require.Equal(t, msg.MsgType, res.MsgType)
	require.True(t, bytes.Equal(msg.Data, res.Data))
}
