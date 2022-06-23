package messages

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// CreateProposalNotPreviouslyPrepared tests creating a proposal msg, non-first round and not previously prepared
func CreateProposalNotPreviouslyPrepared() *tests.CreateMsgSpecTest {
	return &tests.CreateMsgSpecTest{
		CreateType: tests.CreateProposal,
		Name:       "create proposal not previously prepared",
		Value:      []byte{1, 2, 3, 4},
		RoundChangeJustifications: []*qbft.SignedMessage{
			testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
				MsgType:    qbft.RoundChangeMsgType,
				Height:     qbft.FirstHeight,
				Round:      2,
				Identifier: []byte{1, 2, 3, 4},
				Data:       testingutils.RoundChangeDataBytes(nil, qbft.NoRound, []byte{1, 2, 3, 4}),
			}),
			testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[2], types.OperatorID(2), &qbft.Message{
				MsgType:    qbft.RoundChangeMsgType,
				Height:     qbft.FirstHeight,
				Round:      2,
				Identifier: []byte{1, 2, 3, 4},
				Data:       testingutils.RoundChangeDataBytes(nil, qbft.NoRound, []byte{1, 2, 3, 4}),
			}),
			testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[3], types.OperatorID(3), &qbft.Message{
				MsgType:    qbft.RoundChangeMsgType,
				Height:     qbft.FirstHeight,
				Round:      2,
				Identifier: []byte{1, 2, 3, 4},
				Data:       testingutils.RoundChangeDataBytes(nil, qbft.NoRound, []byte{1, 2, 3, 4}),
			}),
		},
		ExpectedRoot: "f910e0dbee145020bdf670ba9c8474b3e4941e528fc08f52e9a3ccbf9b5abd74",
	}
}
