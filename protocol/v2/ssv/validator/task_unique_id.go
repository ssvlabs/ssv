package validator

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
)

func getTaskUniqueID(runner runner.Runner, duty *spectypes.Duty) string {
	epoch := runner.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(duty.Slot)
	return fmt.Sprintf("T:%v::E:%v::S:%v::V:%v", duty.Type.String(), epoch, duty.Slot, duty.ValidatorIndex)
}
