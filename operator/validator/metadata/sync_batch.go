package metadata

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type SyncBatch struct {
	SharesBefore []*ssvtypes.SSVShare
	SharesAfter  []*ssvtypes.SSVShare
	Epoch        phase0.Epoch
}

// DetectValidatorStateChanges compares validator metadata before and after the update to detect state changes.
// Returns validators that became attesting, slashed, or exited.
func (s SyncBatch) DetectValidatorStateChanges() (attesting, slashed, exited []*ssvtypes.SSVShare) {
	// Build a map of previous states for quick lookups
	beforeMap := make(map[spectypes.ValidatorPK]*ssvtypes.SSVShare, len(s.SharesBefore))
	for _, share := range s.SharesBefore {
		beforeMap[share.ValidatorPubKey] = share
	}

	attesting = make([]*ssvtypes.SSVShare, 0, len(s.SharesAfter))
	slashed = make([]*ssvtypes.SSVShare, 0, len(s.SharesAfter))
	exited = make([]*ssvtypes.SSVShare, 0, len(s.SharesAfter))

	for _, shareAfter := range s.SharesAfter {
		shareBefore, exists := beforeMap[shareAfter.ValidatorPubKey]
		if !exists {
			continue
		}

		if !shareBefore.IsAttesting(s.Epoch) && shareAfter.IsAttesting(s.Epoch) {
			attesting = append(attesting, shareAfter)
		}

		if !shareBefore.Slashed() && shareAfter.Slashed() {
			slashed = append(slashed, shareAfter)
		}

		if !shareBefore.Exiting() && shareAfter.Exiting() {
			exited = append(exited, shareAfter)
		}
	}

	return attesting, slashed, exited
}
