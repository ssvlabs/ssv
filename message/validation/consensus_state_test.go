package validation

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestOperatorState(t *testing.T) {
	t.Run("TestNewOperatorState", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size, 2)
		require.NotNil(t, os)
		require.Equal(t, len(os.signers), size)
	})

	t.Run("TestGetAndSet", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size, 2)

		const slot = 5
		const epoch = 1
		signerState := &SignerStateForSlotRound{Slot: slot}

		os.SetSignerStateForSlot(slot, epoch, signerState)
		retrievedState := os.GetSignerStateForSlot(slot)

		require.NotNil(t, retrievedState)
		require.EqualValues(t, retrievedState.Slot, slot)
	})

	t.Run("TestGetInvalidSlot", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size, 2)

		const slot = 5
		retrievedState := os.GetSignerStateForSlot(slot)

		require.Nil(t, retrievedState)
	})

	t.Run("TestMaxSlot", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size, 2)

		const slot = 5
		const epoch = 1
		signerState := &SignerStateForSlotRound{Slot: slot}

		os.SetSignerStateForSlot(slot, epoch, signerState)
		require.EqualValues(t, os.MaxSlot(), slot)
	})

	t.Run("TestDutyCount", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size, 2)

		const slot = 5
		const epoch = 1
		signerState1 := &SignerStateForSlotRound{Slot: slot}

		os.SetSignerStateForSlot(slot, epoch, signerState1)

		require.Equal(t, os.DutyCount(epoch), uint64(1))
		require.Equal(t, os.DutyCount(epoch-1), uint64(0))

		const slot2 = 6
		const epoch2 = 2
		signerState2 := &SignerStateForSlotRound{Slot: slot2}

		os.SetSignerStateForSlot(slot2, epoch2, signerState2)

		require.Equal(t, os.DutyCount(epoch2), uint64(1))
		require.Equal(t, os.DutyCount(epoch), uint64(1))
		require.Equal(t, os.DutyCount(epoch-1), uint64(0))
	})

	t.Run("TestIncrementLastEpochDuties", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size, 2)

		const slot = 5
		const epoch = 1
		signerState1 := &SignerStateForSlotRound{Slot: slot}

		os.SetSignerStateForSlot(slot, epoch, signerState1)
		require.Equal(t, os.DutyCount(epoch), uint64(1))

		const slot2 = 6
		signerState2 := &SignerStateForSlotRound{Slot: slot2}
		os.SetSignerStateForSlot(slot2, epoch, signerState2)

		require.Equal(t, os.DutyCount(epoch), uint64(2))
	})

	// SIP #94 §7: a role whose acceptable slots span several epochs at once keeps a count per epoch, and
	// epochs arriving out of order must not bleed into each other.
	t.Run("TestDutyCountRingAcrossEpochs", func(t *testing.T) {
		os := newOperatorState(10, 4)
		const base = phase0.Epoch(100)
		record := func(slot phase0.Slot, epoch phase0.Epoch) {
			os.SetSignerStateForSlot(slot, epoch, &SignerStateForSlotRound{Slot: slot})
		}

		// Four consecutive epochs, out of order, each keep their own count.
		record(1, base+2)
		record(2, base+2)
		record(3, base)
		record(4, base+1)
		record(5, base-1)
		record(6, base)
		require.Equal(t, uint64(2), os.DutyCount(base+2))
		require.Equal(t, uint64(2), os.DutyCount(base))
		require.Equal(t, uint64(1), os.DutyCount(base+1))
		require.Equal(t, uint64(1), os.DutyCount(base-1))

		// A fifth epoch evicts the oldest: base-1 shares its ring entry with base+3.
		record(7, base+3)
		require.Equal(t, uint64(1), os.DutyCount(base+3))
		require.Equal(t, uint64(0), os.DutyCount(base-1))

		// A message for an epoch older than the entry's occupant is beyond retention: not counted, and
		// the live count is untouched.
		record(8, base-1)
		require.Equal(t, uint64(1), os.DutyCount(base+3))
		require.Equal(t, uint64(0), os.DutyCount(base-1))
	})
}

// The checks that run before a message's signature is verified read an operator's state through
// peekOperatorState, which never allocates: an operator that has sent nothing has no state, and the read
// methods answer for it as for an empty one. Only OperatorState, on the path that records a verified
// message, allocates.
func TestValidatorState_PeekDoesNotAllocate(t *testing.T) {
	cs := &ValidatorState{operators: make([]*OperatorState, 4), storedSlotCount: 66, storedEpochCount: 3}

	var none *OperatorState
	require.Nil(t, cs.peekOperatorState(1))
	require.Equal(t, phase0.Slot(0), none.MaxSlot())
	require.Nil(t, none.GetSignerStateForSlot(5))
	require.Zero(t, none.DutyCount(1))
	require.Nil(t, cs.peekOperatorState(1), "reading allocated nothing")

	allocated := cs.OperatorState(1)
	require.NotNil(t, allocated)
	require.Same(t, allocated, cs.peekOperatorState(1))
	require.Nil(t, cs.peekOperatorState(2), "only the recorded operator has state")
}
