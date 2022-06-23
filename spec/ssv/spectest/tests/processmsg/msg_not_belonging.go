package processmsg

import (
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// MsgNotBelonging tests an SSVMessage ID that doesn't belong to the validator
func MsgNotBelonging() *tests.SpecTest {
	ks := testingutils.Testing4SharesSet()
	dr := testingutils.AttesterRunner(ks)

	msgs := []*types.SSVMessage{
		{
			MsgType: 100,
			MsgID:   types.NewMsgID(testingutils.TestingWrongValidatorPubKey[:], types.BNRoleAttester),
			Data:    []byte{1, 2, 3, 4},
		},
	}

	return &tests.SpecTest{
		Name:                    "ssv msg wrong pubkey in msg id",
		Runner:                  dr,
		Messages:                msgs,
		PostDutyRunnerStateRoot: "c4eb0bb42cc382e468b2362e9d9cc622f388eef6a266901535bb1dfcc51e8868",
		ExpectedError:           "Messages invalid: msg ID doesn't match validator ID",
	}
}
