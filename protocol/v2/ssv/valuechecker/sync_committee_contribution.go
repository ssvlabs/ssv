package valuechecker

import (
	"fmt"

	"github.com/bloxapp/ssv-spec/types"
)

func (vc *ValueChecker) SyncCommitteeContributionValueCheckF(data []byte) error {
	cd := &types.ConsensusData{}
	if err := cd.Decode(data); err != nil {
		return fmt.Errorf("failed decoding consensus data: %w", err)
	}
	if err := cd.Validate(); err != nil {
		return fmt.Errorf("invalid value: %w", err)
	}

	if err := vc.checkDuty(&cd.Duty, types.BNRoleSyncCommitteeContribution); err != nil {
		return fmt.Errorf("duty invalid: %w", err)
	}

	//contributions, _ := cd.GetSyncCommitteeContributions()
	//
	//for _, c := range contributions {
	//	// TODO check we have selection proof for contribution
	//	// TODO check slot == duty slot
	//	// TODO check beacon block root somehow? maybe all beacon block roots should be equal?
	//
	//}
	return nil
}
