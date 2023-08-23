package genesis

import (
	"bytes"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/network/commons"
)

func TestForkV1_Encoding(t *testing.T) {
	msg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   specqbft.ControllerIdToMessageID([]byte("xxxxxxxxxxx_ATTESTER")),
		Data:    []byte("data"),
	}

	b, err := commons.EncodeNetworkMsg(msg)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)

	res, err := commons.DecodeNetworkMsg(b)
	require.NoError(t, err)
	require.Equal(t, msg.MsgType, res.MsgType)
	require.True(t, bytes.Equal(msg.Data, res.Data))
}
