package message

import (
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"testing"
)

func init() {
	logex.Build("test", zapcore.DebugLevel, &logex.EncodingConfig{})
}

func TestChangeRoundV0Root(t *testing.T) {
	identifier := NewIdentifier([]byte("as"), RoleTypeAttester)
	val := []byte("value")

	prepareData := PrepareData{Data: val}
	encodedPrepare, err := prepareData.Encode()
	require.NoError(t, err)

	crm := RoundChangeData{
		PreparedValue:    val,
		Round:            Round(2),
		NextProposalData: nil,
		RoundChangeJustification: []*SignedMessage{
			{
				Signature: []byte("sig"),
				Signers:   []OperatorID{1, 2, 3, 4},
				Message: &ConsensusMessage{
					MsgType:    PrepareMsgType,
					Height:     Height(1),
					Round:      Round(1),
					Identifier: identifier,
					Data:       encodedPrepare,
				},
			},
		},
	}

	crmEncoded, err := crm.Encode()
	require.NoError(t, err)
	cm := ConsensusMessage{
		MsgType:    RoundChangeMsgType,
		Height:     Height(1),
		Round:      Round(2),
		Identifier: identifier,
		Data:       crmEncoded,
	}
	_, err = cm.GetRoot(forksprotocol.V0ForkVersion.String()) // TODO need to add the v0 real root to compare
	require.NoError(t, err)
}

func TestDecidedV0Root(t *testing.T) {
	identifier := NewIdentifier([]byte("as"), RoleTypeAttester)
	val := []byte("value")
	commit := CommitData{Data: val}
	crmEncoded, err := commit.Encode()
	require.NoError(t, err)
	cm := ConsensusMessage{
		MsgType:    CommitMsgType,
		Height:     Height(1),
		Round:      Round(2),
		Identifier: identifier,
		Data:       crmEncoded,
	}

	cm.GetRoot("v0") // TODO need to add the v0 real root to compare
}
