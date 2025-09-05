package fields

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/observability/utils"
)

func BuildDutyID(epoch phase0.Epoch, slot phase0.Slot, runnerRole spectypes.RunnerRole, index phase0.ValidatorIndex) string {
	return fmt.Sprintf("%v-e%v-s%v-v%v", utils.FormatRunnerRole(runnerRole), epoch, slot, index)
}

func BuildCommitteeDutyID(operators []spectypes.OperatorID, epoch phase0.Epoch, slot phase0.Slot) string {
	return fmt.Sprintf("COMMITTEE-%s-e%d-s%d", utils.FormatCommittee(operators), epoch, slot)
}
