package messages

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// SignedMsgSigTooLong tests SignedMessage len(signature) > 96
func SignedMsgSigTooLong() *tests.MsgSpecTest {
	msg := testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
		MsgType:    qbft.CommitMsgType,
		Height:     qbft.FirstHeight,
		Round:      qbft.FirstRound,
		Identifier: []byte{1, 2, 3, 4},
		Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
	})
	msg.Signature = make([]byte, 97)

	return &tests.MsgSpecTest{
		Name: "signature too long",
		Messages: []*qbft.SignedMessage{
			msg,
		},
		ExpectedError: "message signature is invalid",
	}
}
