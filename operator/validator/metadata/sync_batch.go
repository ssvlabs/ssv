package metadata

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

// SyncBatch represents the validator metadata before and after a sync.
type SyncBatch struct {
	Before beacon.ValidatorMetadataMap
	After  beacon.ValidatorMetadataMap
}

// DetectValidatorStateChanges identifies validators whose states changed between syncs.
// Specifically returns validators that:
// - became eligible to start (transitioned from Unknown to any known state)
// - became slashed (transitioned to a slashed state)
// - became exited (transitioned to an exited state)
func (s SyncBatch) DetectValidatorStateChanges() (eligibleToStart, slashed, exited []spectypes.ValidatorPK) {
	eligibleToStart = make([]spectypes.ValidatorPK, 0, len(s.After))
	slashed = make([]spectypes.ValidatorPK, 0, len(s.After))
	exited = make([]spectypes.ValidatorPK, 0, len(s.After))

	for pk, after := range s.After {
		before, exists := s.Before[pk]
		if !exists {
			continue
		}

		if before.Unknown() && !after.Unknown() {
			eligibleToStart = append(eligibleToStart, pk)
		}

		if !before.Slashed() && after.Slashed() {
			slashed = append(slashed, pk)
		}

		if !before.Exited() && after.Exited() {
			exited = append(exited, pk)
		}
	}

	return eligibleToStart, slashed, exited
}
