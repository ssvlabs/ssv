// runner declares logging fields used by the github.com/bloxapp/ssv/protocol/v2/ssv/runner package. this is a
// separate package from github.com/bloxapp/ssv/logging/fields in order to avoid an import cycle
package runner

import (
	"fmt"

	"github.com/bloxapp/ssv/logging/fields/stringer"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"go.uber.org/zap"
)

const FieldDutyID = "duty_id"

func DutyID(dutyRunner runner.Runner) zap.Field {
	return zap.Stringer(FieldDutyID, stringer.FuncStringer{
		Fn: func() string {
			startingDuty := dutyRunner.GetBaseRunner().State.StartingDuty

			dutyType := startingDuty.Type.String()
			epoch := dutyRunner.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(startingDuty.Slot)
			slot := startingDuty.Slot
			validatorIndex := startingDuty.ValidatorIndex

			return fmt.Sprintf("%v-e%v-s%v-v%v", dutyType, epoch, slot, validatorIndex)
		},
	})
}
