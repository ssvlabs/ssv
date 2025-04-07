package metadata

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"

	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

// SyncBatch represents the validator metadata before and after a sync.
type SyncBatch struct {
	Before registrystorage.ValidatorMetadataMap
	After  registrystorage.ValidatorMetadataMap
}

// DetectValidatorStateChanges identifies validators whose states changed between syncs.
// Specifically returns validators that:
// - became eligible to start (transitioned from Unknown to Active without being slashed/exited)
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

		if after.BecameEligible(before) {
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
