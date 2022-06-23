package messages

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// DecidedMsgEncoding tests encoding DecidedMessage
func DecidedMsgEncoding() *tests.DecidedMsgSpecTest {
	msg := &qbft.DecidedMessage{
		SignedMessage: testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		}),
	}

	b, _ := msg.Encode()

	return &tests.DecidedMsgSpecTest{
		Name: "decided msg encoding",
		Messages: []*qbft.DecidedMessage{
			msg,
		},
		EncodedMessages: [][]byte{
			b,
		},
	}
}
