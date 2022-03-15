package v0

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestForkV0_Encoding(t *testing.T) {
	msg := &network.Message{
		SignedMessage: &proto.SignedMessage{Message: &proto.Message{Type: proto.RoundState_Decided}},
		Type:          network.NetworkMsg_DecidedType,
	}
	f := &ForkV0{}

	b, err := f.EncodeNetworkMsg(msg)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)

	res, err := f.DecodeNetworkMsg(b)
	require.NoError(t, err)
	require.Equal(t, msg.Type, res.(*network.Message).Type)
}
