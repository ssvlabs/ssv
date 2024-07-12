package validation

import (
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// consensusID uniquely identifies a public key and role pair to keep track of state.
type consensusID struct {
	DutyExecutorID string
	Role           spectypes.RunnerRole
}

// consensusState keeps track of the signers for a given public key and role.
type consensusState struct {
	state           map[spectypes.OperatorID]*OperatorState
	storedSlotCount phase0.Slot
	mu              sync.Mutex
}

func (cs *consensusState) GetOrCreate(signer spectypes.OperatorID) *OperatorState {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if _, ok := cs.state[signer]; !ok {
		cs.state[signer] = newOperatorState(cs.storedSlotCount)
	}

	return cs.state[signer]
}

type OperatorState struct {
	state           []*SignerState // the slice index is slot % storedSlotCount
	maxSlot         phase0.Slot
	maxEpoch        phase0.Epoch
	lastEpochDuties int
	prevEpochDuties int
}

func newOperatorState(size phase0.Slot) *OperatorState {
	return &OperatorState{
		state: make([]*SignerState, size),
	}
}

func (os *OperatorState) Get(slot phase0.Slot) *SignerState {
	s := os.state[int(slot)%len(os.state)]
	if s == nil || s.Slot != slot {
		return nil
	}

	return s
}

func (os *OperatorState) Set(slot phase0.Slot, epoch phase0.Epoch, state *SignerState) {
	os.state[int(slot)%len(os.state)] = state
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

func (os *OperatorState) DutyCount(epoch phase0.Epoch) int {
	if epoch == os.maxEpoch {
		return os.lastEpochDuties
	}
	if epoch == os.maxEpoch-1 {
		return os.prevEpochDuties
	}
	return 0 // unused because messages from too old epochs must be rejected in advance
}
