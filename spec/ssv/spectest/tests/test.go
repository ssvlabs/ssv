package tests

import (
	"github.com/bloxapp/ssv/spec/ssv"
	"github.com/bloxapp/ssv/spec/types"
)

type SpecTest struct {
	Name                    string
	Runner                  *ssv.Runner
	Duty                    *types.Duty
	Messages                []*types.SSVMessage
	PostDutyRunnerStateRoot string
	ExpectedError           string
}
