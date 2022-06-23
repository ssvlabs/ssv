package messages

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// CreateProposalPreviouslyPrepared tests creating a proposal msg,previously prepared
func CreateProposalPreviouslyPrepared() *tests.CreateMsgSpecTest {
	return &tests.CreateMsgSpecTest{
		CreateType: tests.CreateProposal,
		Name:       "create proposal previously prepared",
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
		PrepareJustifications: []*qbft.SignedMessage{
			testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
				MsgType:    qbft.PrepareMsgType,
				Height:     qbft.FirstHeight,
				Round:      qbft.FirstRound,
				Identifier: []byte{1, 2, 3, 4},
				Data:       testingutils.PrepareDataBytes([]byte{1, 2, 3, 4}),
			}),
			testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[2], types.OperatorID(2), &qbft.Message{
				MsgType:    qbft.PrepareMsgType,
				Height:     qbft.FirstHeight,
				Round:      qbft.FirstRound,
				Identifier: []byte{1, 2, 3, 4},
				Data:       testingutils.PrepareDataBytes([]byte{1, 2, 3, 4}),
			}),
			testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[3], types.OperatorID(3), &qbft.Message{
				MsgType:    qbft.PrepareMsgType,
				Height:     qbft.FirstHeight,
				Round:      qbft.FirstRound,
				Identifier: []byte{1, 2, 3, 4},
				Data:       testingutils.PrepareDataBytes([]byte{1, 2, 3, 4}),
			}),
		},
		ExpectedRoot: "17edc6990c2b955ccc4945f63b5d5a920dd5851d408dc0a335780d7fdaeb8cf3",
	}
}
