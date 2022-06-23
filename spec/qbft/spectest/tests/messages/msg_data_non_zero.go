package messages

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// MsgDataNonZero tests len(data) == 0
func MsgDataNonZero() *tests.MsgSpecTest {
	msg := testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
		MsgType:    qbft.ProposalMsgType,
		Height:     qbft.FirstHeight,
		Round:      qbft.FirstRound,
		Identifier: []byte{1, 2, 3, 4},
		Data:       []byte{},
	})

	return &tests.MsgSpecTest{
		Name: "msg data len 0",
		Messages: []*qbft.SignedMessage{
			msg,
		},
		ExpectedError: "message data is invalid",
	}
}
