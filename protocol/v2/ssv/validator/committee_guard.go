package validator

import (
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// CommitteeDutyGuard helps guarantee exclusive execution of one duty per validator
// and non-execution of stopped validators.
type CommitteeDutyGuard struct {
	duties map[spectypes.BeaconRole]map[spectypes.ValidatorPK]phase0.Slot
	mu     sync.RWMutex
}

func NewCommitteeDutyGuard() *CommitteeDutyGuard {
	return &CommitteeDutyGuard{
		duties: map[spectypes.BeaconRole]map[spectypes.ValidatorPK]phase0.Slot{
			spectypes.BNRoleAttester:      {},
			spectypes.BNRoleSyncCommittee: {},
		},
	}
}

// StartDuty records a started duty. If a duty is already running at the same or higher slot, it returns an error.
func (a *CommitteeDutyGuard) StartDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	duties, ok := a.duties[role]
	if !ok {
		return fmt.Errorf("unsupported role %d", role)
	}
	// If an older committee duty is still running for this validator we won't be interested in it
	// anymore now that we have a fresher duty started. The older duty might or might not finish
	// successfully, either outcome is fine but since it's always better to execute the freshest
	// committee duty CommitteeDutyGuard will invalidate the older duty (potentially preventing it
	// from execution so that we don't waste resources on it).
	runningSlot, exists := duties[validator]
	if exists && runningSlot >= slot {
		return fmt.Errorf("duty already running at slot %d", runningSlot)
	}
	duties[validator] = slot
	return nil
}

// StopValidator removes any running duties for a validator.
func (a *CommitteeDutyGuard) StopValidator(validator spectypes.ValidatorPK) {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.duties[spectypes.BNRoleAttester], validator)
	delete(a.duties[spectypes.BNRoleSyncCommittee], validator)
}

// ValidDuty checks if a duty is still valid for execution.
func (a *CommitteeDutyGuard) ValidDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	duties, ok := a.duties[role]
	if !ok {
		return fmt.Errorf("unsupported role %d", role)
	}
	runningSlot, exists := duties[validator]
	if !exists {
		return fmt.Errorf("duty not found")
	}
	if runningSlot != slot {
		return fmt.Errorf("slot mismatch: duty is running at slot %d", runningSlot)
	}
	return nil
}
