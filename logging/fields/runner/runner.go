// runner declares logging fields used by the github.com/bloxapp/ssv/protocol/v2/ssv/runner package. this is a
// separate package from github.com/bloxapp/ssv/logging/fields in order to avoid an import cycle
package runner

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging/fields/stringer"
	"go.uber.org/zap"
)

const FieldDutyID = "duty_id"

func DutyID(beaconNetwork spectypes.BeaconNetwork, duty *spectypes.Duty) zap.Field {
	return zap.Stringer(FieldDutyID, stringer.FuncStringer{
		Fn: func() string {
			dutyType := duty.Type.String()
			epoch := beaconNetwork.EstimatedEpochAtSlot(duty.Slot)
			slot := duty.Slot
			validatorIndex := duty.ValidatorIndex

			return fmt.Sprintf("%v-e%v-s%v-v%v", dutyType, epoch, slot, validatorIndex)
		},
	})
}
