package processmsg

import (
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// NoData tests a SSVMessage with no data
func NoData() *tests.SpecTest {
	dr := testingutils.AttesterRunner()

	msgs := []*types.SSVMessage{
		{
			MsgType: types.SSVConsensusMsgType,
			MsgID:   types.NewMsgID(testingutils.TestingValidatorPubKey[:], beacon.RoleTypeAttester),
			Data:    nil,
		},
	}

	return &tests.SpecTest{
		Name:                    "ssv msg no data",
		Runner:                  dr,
		Messages:                msgs,
		PostDutyRunnerStateRoot: "c4eb0bb42cc382e468b2362e9d9cc622f388eef6a266901535bb1dfcc51e8868",
		ExpectedError:           "Messages invalid: msg data is invalid",
	}
}
