package genesis

import (
	"bytes"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/stretchr/testify/require"
)

func TestForkV1_Encoding(t *testing.T) {
	msg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   specqbft.ControllerIdToMessageID([]byte("xxxxxxxxxxx_ATTESTER")),
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
