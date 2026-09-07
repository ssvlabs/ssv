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
	// storedEpochCount defines how many epochs of duty counts OperatorState keeps (see storedEpochCount).
	storedEpochCount uint64
}

func (cs *ValidatorState) OperatorState(operatorIdx int) *OperatorState {
	if cs.operators[operatorIdx] == nil {
		cs.operators[operatorIdx] = newOperatorState(cs.storedSlotCount, cs.storedEpochCount)
	}

	return cs.operators[operatorIdx]
}

type OperatorState struct {
	// signers stores the latest ValidatorState.storedSlotCount signers, signer corresponding to
	// slot s is residing at index s % ValidatorState.storedSlotCount
	signers []*SignerStateForSlotRound
	maxSlot phase0.Slot
	// duties counts the signer's distinct duty slots per epoch, one ring entry per epoch at index
	// epoch % ValidatorState.storedEpochCount. The ring must span every epoch whose slots are still
	// acceptable, or the per-epoch duty limit silently re-opens (SIP #94 §7).
	duties []epochDuties
}

// epochDuties is one ring entry of OperatorState.duties: count is 0 while the entry is unused.
type epochDuties struct {
	epoch phase0.Epoch
	count uint64
}

func newOperatorState(slotCount, epochCount uint64) *OperatorState {
	return &OperatorState{
		signers: make([]*SignerStateForSlotRound, slotCount),
		duties:  make([]epochDuties, epochCount),
	}
}

func (os *OperatorState) GetSignerStateForSlot(slot phase0.Slot) *SignerStateForSlotRound {
	s := os.signers[(uint64(slot) % uint64(len(os.signers)))]
	if s == nil || s.Slot != slot {
		return nil
	}

	return s
}

// SetSignerStateForSlot records the first accepted message of a new duty slot: it stores the slot's
// signer state and counts the slot toward its epoch's duty count.
func (os *OperatorState) SetSignerStateForSlot(slot phase0.Slot, epoch phase0.Epoch, state *SignerStateForSlotRound) {
	os.signers[uint64(slot)%uint64(len(os.signers))] = state
	if slot > os.maxSlot {
		os.maxSlot = slot
	}
	os.countDuty(epoch)
}

// countDuty adds one duty to the epoch's count. A newer epoch landing on an occupied ring entry evicts
// the epoch that occupies it, which is then older than the ring spans; a message for an epoch older than
// the entry's occupant is beyond retention (the lateness rule should already have dropped it) and is not
// counted rather than corrupting the live count.
func (os *OperatorState) countDuty(epoch phase0.Epoch) {
	entry := &os.duties[uint64(epoch)%uint64(len(os.duties))]
	switch {
	case entry.count == 0 || epoch > entry.epoch:
		*entry = epochDuties{epoch: epoch, count: 1}
	case epoch == entry.epoch:
		entry.count++
	}
}

func (os *OperatorState) MaxSlot() phase0.Slot {
	return os.maxSlot
}

// DutyCount returns the signer's distinct duty slots counted for the epoch; 0 for an epoch the ring no
// longer (or never) holds.
func (os *OperatorState) DutyCount(epoch phase0.Epoch) uint64 {
	entry := os.duties[uint64(epoch)%uint64(len(os.duties))]
	if entry.count == 0 || entry.epoch != epoch {
		return 0
	}
	return entry.count
}
