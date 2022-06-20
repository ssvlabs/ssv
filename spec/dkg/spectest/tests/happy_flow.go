package tests

import (
	"github.com/bloxapp/ssv/spec/dkg"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// HappyFlow tests a simple full happy flow until decided
func HappyFlow() *MsgProcessingSpecTest {
	testingutils.Testing4SharesSet()
	return &MsgProcessingSpecTest{
		Name: "happy flow",
		Messages: []*dkg.SignedMessage{
			testingutils.SignDKGMsg(testingutils.Testing4SharesSet().DKGOperators[1].SK, 1, &dkg.Message{
				MsgType:    dkg.InitMsgType,
				Identifier: dkg.NewRequestID(testingutils.Testing4SharesSet().DKGOperators[1].ETHAddress, 1),
				Data:       testingutils.InitMessageDataBytes([]types.OperatorID{1, 2, 3, 4}, 3, testingutils.TestingWithdrawalCredentials),
			}),
		},
	}
}
