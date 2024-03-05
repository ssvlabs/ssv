package valuechecker

import (
	"fmt"

	"github.com/bloxapp/ssv-spec/types"
)

func (vc *ValueChecker) AggregatorValueCheckF(data []byte) error {
	cd := &types.ConsensusData{}
	if err := cd.Decode(data); err != nil {
		return fmt.Errorf("failed decoding consensus data: %w", err)
	}
	if err := cd.Validate(); err != nil {
		return fmt.Errorf("invalid value: %w", err)
	}

	if err := vc.checkDuty(&cd.Duty, types.BNRoleAggregator); err != nil {
		return fmt.Errorf("invalid duty: %w", err)
	}

	return nil
}
