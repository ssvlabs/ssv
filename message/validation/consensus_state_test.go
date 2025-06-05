package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOperatorState(t *testing.T) {
	t.Run("TestNewOperatorState", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size)
		require.NotNil(t, os)
		require.Equal(t, len(os.signers), size)
	})

	t.Run("TestGetAndSet", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size)

		const slot = 5
		const epoch = 1
		signerState := &SignerState{Slot: slot}

		os.SetSignerState(slot, epoch, signerState)
		retrievedState := os.GetSignerState(slot)

		require.NotNil(t, retrievedState)
		require.EqualValues(t, retrievedState.Slot, slot)
	})

	t.Run("TestGetInvalidSlot", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size)

		const slot = 5
		retrievedState := os.GetSignerState(slot)

		require.Nil(t, retrievedState)
	})

	t.Run("TestMaxSlot", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size)

		const slot = 5
		const epoch = 1
		signerState := &SignerState{Slot: slot}

		os.SetSignerState(slot, epoch, signerState)
		require.EqualValues(t, os.MaxSlot(), slot)
	})

	t.Run("TestDutyCount", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size)

		const slot = 5
		const epoch = 1
		signerState1 := &SignerState{Slot: slot}

		os.SetSignerState(slot, epoch, signerState1)

		require.Equal(t, os.DutyCount(epoch), uint64(1))
		require.Equal(t, os.DutyCount(epoch-1), uint64(0))

		const slot2 = 6
		const epoch2 = 2
		signerState2 := &SignerState{Slot: slot2}

		os.SetSignerState(slot2, epoch2, signerState2)

		require.Equal(t, os.DutyCount(epoch2), uint64(1))
		require.Equal(t, os.DutyCount(epoch), uint64(1))
		require.Equal(t, os.DutyCount(epoch-1), uint64(0))
	})

	t.Run("TestIncrementLastEpochDuties", func(t *testing.T) {
		const size = 10
		os := newOperatorState(size)

		const slot = 5
		const epoch = 1
		signerState1 := &SignerState{Slot: slot}

		os.SetSignerState(slot, epoch, signerState1)
		require.Equal(t, os.DutyCount(epoch), uint64(1))

		const slot2 = 6
		signerState2 := &SignerState{Slot: slot2}
		os.SetSignerState(slot2, epoch, signerState2)

		require.Equal(t, os.DutyCount(epoch), uint64(2))
	})
}
