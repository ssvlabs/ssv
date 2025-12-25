package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// ValidatorState keeps track of the signers for a given public key and role.
type ValidatorState struct {
	operators       []*OperatorState
	storedSlotCount uint64
}

func (cs *ValidatorState) OperatorState(operatorIdx int) *OperatorState {
	if cs.operators[operatorIdx] == nil {
		cs.operators[operatorIdx] = newOperatorState(cs.storedSlotCount)
	}

	return cs.operators[operatorIdx]
}

type OperatorState struct {
	// signers stores the latest ValidatorState.storedSlotCount signers, signer corresponding to
	// slot s is residing at index s % ValidatorState.storedSlotCount
	signers         []*SignerState
	maxSlot         phase0.Slot
	maxEpoch        phase0.Epoch
	currEpochDuties uint64
	prevEpochDuties uint64
}

func newOperatorState(size uint64) *OperatorState {
	return &OperatorState{
		signers: make([]*SignerState, size),
	}
}

func (os *OperatorState) GetSignerStateForSlot(slot phase0.Slot) *SignerState {
	s := os.signers[(uint64(slot) % uint64(len(os.signers)))]
	if s == nil || s.Slot != slot {
		return nil
	}

	return s
}

func (os *OperatorState) SetSignerStateForSlot(slot phase0.Slot, epoch phase0.Epoch, state *SignerState) {
	os.signers[uint64(slot)%uint64(len(os.signers))] = state
	if slot > os.maxSlot {
		os.maxSlot = slot
	}
	if epoch > os.maxEpoch {
		os.maxEpoch = epoch
		os.prevEpochDuties = os.currEpochDuties
		os.currEpochDuties = 1
	} else if epoch == os.maxEpoch {
		os.currEpochDuties++
	} else {
		os.prevEpochDuties++
	}
}

func (os *OperatorState) MaxSlot() phase0.Slot {
	return os.maxSlot
}

func (os *OperatorState) DutyCount(epoch phase0.Epoch) uint64 {
	if epoch == os.maxEpoch {
		return os.currEpochDuties
	}
	if epoch == os.maxEpoch-1 {
		return os.prevEpochDuties
	}
	return 0 // unused because messages from too old epochs must be rejected in advance
}
