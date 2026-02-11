package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// ValidatorState keeps track of signers(operators) for some validator.
type ValidatorState struct {
	// committeeID is the ID of the committee this validator currently belongs to
	committeeID spectypes.CommitteeID

	// operators is a list of operators in the committee this validator currently belongs to
	operators []*OperatorState

	// storedSlotCount defines how many recent slots we want to store in OperatorState
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
	signers         []*SignerStateForSlotRound
	maxSlot         phase0.Slot
	maxEpoch        phase0.Epoch
	currEpochDuties uint64
	prevEpochDuties uint64
}

func newOperatorState(size uint64) *OperatorState {
	return &OperatorState{
		signers: make([]*SignerStateForSlotRound, size),
	}
}

func (os *OperatorState) GetSignerStateForSlot(slot phase0.Slot) *SignerStateForSlotRound {
	s := os.signers[(uint64(slot) % uint64(len(os.signers)))]
	if s == nil || s.Slot != slot {
		return nil
	}

	return s
}

func (os *OperatorState) SetSignerStateForSlot(slot phase0.Slot, epoch phase0.Epoch, state *SignerStateForSlotRound) {
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
