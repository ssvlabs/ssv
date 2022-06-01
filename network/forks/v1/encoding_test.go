package v1

import (
	"bytes"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestForkV1_Encoding(t *testing.T) {
	msg := &message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      []byte("xxxxxxxxxxx_ATTESTER"),
		Data:    []byte("data"),
	}
	f := &ForkV1{}

	b, err := f.EncodeNetworkMsg(msg)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)

	res, err := f.DecodeNetworkMsg(b)
	require.NoError(t, err)
	require.Equal(t, msg.MsgType, res.MsgType)
	require.True(t, bytes.Equal(msg.Data, res.Data))
}
