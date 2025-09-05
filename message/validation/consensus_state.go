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
	storedSlotCount uint64
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
	mu              sync.RWMutex
	state           []*SignerState // the slice index is slot % storedSlotCount
	maxSlot         phase0.Slot
	maxEpoch        phase0.Epoch
	currEpochDuties uint64
	prevEpochDuties uint64
}

func newOperatorState(size uint64) *OperatorState {
	return &OperatorState{
		state: make([]*SignerState, size),
	}
}

func (os *OperatorState) GetSignerState(slot phase0.Slot) *SignerState {
	os.mu.RLock()
	defer os.mu.RUnlock()

	s := os.state[(uint64(slot) % uint64(len(os.state)))]
	if s == nil || s.Slot != slot {
		return nil
	}

	return s
}

func (os *OperatorState) SetSignerState(slot phase0.Slot, epoch phase0.Epoch, state *SignerState) {
	os.mu.Lock()
	defer os.mu.Unlock()

	os.state[uint64(slot)%uint64(len(os.state))] = state
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
	os.mu.RLock()
	defer os.mu.RUnlock()

	return os.maxSlot
}

func (os *OperatorState) DutyCount(epoch phase0.Epoch) uint64 {
	os.mu.RLock()
	defer os.mu.RUnlock()

	if epoch == os.maxEpoch {
		return os.currEpochDuties
	}
	if epoch == os.maxEpoch-1 {
		return os.prevEpochDuties
	}
	return 0 // unused because messages from too old epochs must be rejected in advance
}
