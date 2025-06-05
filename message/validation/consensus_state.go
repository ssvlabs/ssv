package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// ValidatorState keeps track of the signers for a given public key and role.
type ValidatorState struct {
	operators       []*OperatorState
	storedSlotCount uint64
}

func (cs *ValidatorState) Signer(idx int) *OperatorState {
	if cs.operators[idx] == nil {
		cs.operators[idx] = newOperatorState(cs.storedSlotCount)
	}

	return cs.operators[idx]
}

type OperatorState struct {
	signers         []*SignerState // the slice index is slot % storedSlotCount
	maxSlot         phase0.Slot
	maxEpoch        phase0.Epoch
	lastEpochDuties uint64
	prevEpochDuties uint64
}

func newOperatorState(size uint64) *OperatorState {
	return &OperatorState{
		signers: make([]*SignerState, size),
	}
}

func (os *OperatorState) GetSignerState(slot phase0.Slot) *SignerState {
	s := os.signers[(uint64(slot) % uint64(len(os.signers)))]
	if s == nil || s.Slot != slot {
		return nil
	}

	return s
}

func (os *OperatorState) SetSignerState(slot phase0.Slot, epoch phase0.Epoch, state *SignerState) {
	os.signers[uint64(slot)%uint64(len(os.signers))] = state
	if slot > os.maxSlot {
		os.maxSlot = slot
	}
	if epoch > os.maxEpoch {
		os.maxEpoch = epoch
		os.prevEpochDuties = os.lastEpochDuties
		os.lastEpochDuties = 1
	} else {
		os.lastEpochDuties++
	}
}

func (os *OperatorState) MaxSlot() phase0.Slot {
	return os.maxSlot
}

func (os *OperatorState) DutyCount(epoch phase0.Epoch) uint64 {
	if epoch == os.maxEpoch {
		return os.lastEpochDuties
	}
	if epoch == os.maxEpoch-1 {
		return os.prevEpochDuties
	}
	return 0 // unused because messages from too old epochs must be rejected in advance
}
