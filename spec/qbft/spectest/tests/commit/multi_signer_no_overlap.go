package commit

import (
	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// MultiSignerNoOverlap tests a multi signer commit msg which doesn't overlap previous valid commits
func MultiSignerNoOverlap() *tests.MsgProcessingSpecTest {
	pre := testingutils.BaseInstance()
	msgs := []*qbft.SignedMessage{
		testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
			MsgType:    qbft.ProposalMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.ProposalDataBytes([]byte{1, 2, 3, 4}, nil, nil),
		}),
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
		testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		}),
		testingutils.MultiSignQBFTMsg([]*bls.SecretKey{testingutils.Testing4SharesSet().Shares[2], testingutils.Testing4SharesSet().Shares[3]}, []types.OperatorID{2, 3}, &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		}),
	}
	return &tests.MsgProcessingSpecTest{
		Name:          "multi signer, no overlap",
		Pre:           pre,
		PostRoot:      "06d5ce6451794fb52e39a83d48aa559b9d23ce7241be9d70d33968f4bf8c3000",
		InputMessages: msgs,
	}
}
