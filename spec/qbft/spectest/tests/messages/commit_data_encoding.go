package messages

import (
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
"
"github.com/bloxapp/ssv/spec/qbft"
"github.com/bloxapp/ssv/spec/types"
"github.com/bloxapp/ssv/spec/types/testingutils"
)

// CommitDataEncoding tests encoding CommitData
func CommitDataEncoding() *tests.MsgSpecTest {
	msg := testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
		MsgType:    qbft.CommitMsgType,
		Height:     qbft.FirstHeight,
		Round:      qbft.FirstRound,
		Identifier: []byte{1, 2, 3, 4},
		Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
	})

	r, _ := msg.GetRoot()
	b, _ := msg.Encode()

	return &tests.MsgSpecTest{
		Name: "commit data nil or len 0",
		Messages: []*qbft.SignedMessage{
			msg,
		},
		EncodedMessages: [][]byte{
			b,
		},
		ExpectedRoots: [][]byte{
			r,
		},
	}
}
