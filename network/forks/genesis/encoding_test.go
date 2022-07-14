package genesis

import (
	"bytes"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func TestForkV1_Encoding(t *testing.T) {
	msg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   message.ToMessageID([]byte("xxxxxxxxxxx_ATTESTER")),
		Data:    []byte("data"),
	}
	f := &ForkGenesis{}

	b, err := f.EncodeNetworkMsg(msg)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)

	res, err := f.DecodeNetworkMsg(b)
	require.NoError(t, err)
	require.Equal(t, msg.MsgType, res.MsgType)
	require.True(t, bytes.Equal(msg.Data, res.Data))
}
