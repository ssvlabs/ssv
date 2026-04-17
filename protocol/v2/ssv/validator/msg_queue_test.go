package validator

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/v2/protocol/v2/message"
	"github.com/ssvlabs/ssv/v2/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/v2/protocol/v2/types"
)

func TestMKey_Format(t *testing.T) {
	t.Parallel()

	var msgID spectypes.MessageID
	for i := range msgID {
		msgID[i] = byte(i)
	}
	msgIDHex := hex.EncodeToString(msgID[:])

	timeoutDataBytes, err := json.Marshal(&ssvtypes.TimeoutData{
		Height: specqbft.Height(12345),
		Round:  specqbft.Round(7),
	})
	require.NoError(t, err)

	executeDutyBytes, err := json.Marshal(&ssvtypes.ExecuteDutyData{
		Duty: &spectypes.ValidatorDuty{
			Slot: phase0.Slot(54321),
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		msg      *queue.SSVMessage
		expected string
	}{
		{
			name: "event/timeout",
			msg: &queue.SSVMessage{
				SSVMessage: &spectypes.SSVMessage{
					MsgType: message.SSVEventMsgType,
					MsgID:   msgID,
				},
				Body: &ssvtypes.EventMsg{
					Type: ssvtypes.Timeout,
					Data: timeoutDataBytes,
				},
			},
			expected: fmt.Sprintf("%d-%d-%d-%d-%s", 12345, message.SSVEventMsgType, ssvtypes.Timeout, 7, msgIDHex),
		},
		{
			name: "event/execute-duty",
			msg: &queue.SSVMessage{
				SSVMessage: &spectypes.SSVMessage{
					MsgType: message.SSVEventMsgType,
					MsgID:   msgID,
				},
				Body: &ssvtypes.EventMsg{
					Type: ssvtypes.ExecuteDuty,
					Data: executeDutyBytes,
				},
			},
			expected: fmt.Sprintf("%d-%d-%d-%d-%s", 54321, message.SSVEventMsgType, ssvtypes.ExecuteDuty, 0, msgIDHex),
		},
		{
			name: "qbft/consensus",
			msg: &queue.SSVMessage{
				SSVMessage: &spectypes.SSVMessage{
					MsgType: spectypes.SSVConsensusMsgType,
					MsgID:   msgID,
				},
				SignedSSVMessage: &spectypes.SignedSSVMessage{
					OperatorIDs: []spectypes.OperatorID{1, 22, 333},
				},
				Body: &specqbft.Message{
					MsgType: specqbft.PrepareMsgType,
					Height:  specqbft.Height(777),
					Round:   specqbft.Round(3),
				},
			},
			expected: fmt.Sprintf(
				"%d-%d-%d-%d-%s-%s",
				777,
				spectypes.SSVConsensusMsgType,
				specqbft.PrepareMsgType,
				3,
				msgIDHex,
				"[1-22-333]",
			),
		},
		{
			name: "partial-sig",
			msg: &queue.SSVMessage{
				SSVMessage: &spectypes.SSVMessage{
					MsgType: spectypes.SSVPartialSignatureMsgType,
					MsgID:   msgID,
				},
				Body: &spectypes.PartialSignatureMessages{
					Type: spectypes.RandaoPartialSig,
					Slot: phase0.Slot(888),
					Messages: []*spectypes.PartialSignatureMessage{
						{
							Signer: 42,
						},
					},
				},
			},
			expected: fmt.Sprintf("%d-%d-%d-%s-%d", 888, spectypes.SSVPartialSignatureMsgType, spectypes.RandaoPartialSig, msgIDHex, 42),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			key, err := mKey(tt.msg)
			require.NoError(t, err)
			require.Equal(t, tt.expected, string(key))
		})
	}
}
