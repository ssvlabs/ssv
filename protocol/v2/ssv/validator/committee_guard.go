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

func (a *CommitteeDutyGuard) StartDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	duties, ok := a.duties[role]
	if !ok {
		return fmt.Errorf("unknown role %d", role)
	}
	runningSlot, exists := duties[validator]
	if exists && runningSlot >= slot {
		return fmt.Errorf("duty already running at slot %d", runningSlot)
	}
	duties[validator] = slot
	return nil
}

func (a *CommitteeDutyGuard) StopValidator(validator spectypes.ValidatorPK) {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.duties[spectypes.BNRoleAttester], validator)
	delete(a.duties[spectypes.BNRoleSyncCommittee], validator)
}

func (a *CommitteeDutyGuard) ValidDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	duties, ok := a.duties[role]
	if !ok {
		return fmt.Errorf("unknown role %d", role)
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
