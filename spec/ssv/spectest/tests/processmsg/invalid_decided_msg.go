package processmsg

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// InvalidDecidedMsg tests an invalid decided SSVMessage data
func InvalidDecidedMsg() *tests.SpecTest {
	dr := testingutils.AttesterRunner()

	msgs := []*types.SSVMessage{
		{
			MsgType: types.SSVDecidedMsgType,
			MsgID:   types.NewMsgID(testingutils.TestingValidatorPubKey[:], beacon.RoleTypeAttester),
			Data:    []byte{1, 2, 3, 4},
		},
	}

	return &tests.SpecTest{
		Name:                    "ssv msg invalid decided data",
		Runner:                  dr,
		Messages:                msgs,
		PostDutyRunnerStateRoot: "c4eb0bb42cc382e468b2362e9d9cc622f388eef6a266901535bb1dfcc51e8868",
		ExpectedError:           "could not get decided Message from network Message: invalid character '\\x01' looking for beginning of value",
	}
}
