package validation

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestOperatorState(t *testing.T) {
	t.Run("TestNewOperatorState", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)
		require.NotNil(t, os)
		require.Equal(t, len(os.state), int(size))
	})

	t.Run("TestGetAndSet", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		epoch := phase0.Epoch(1)
		signerState := &SignerState{Slot: slot}

		os.Set(slot, epoch, signerState)
		retrievedState := os.Get(slot)

		require.NotNil(t, retrievedState)
		require.Equal(t, retrievedState.Slot, slot)
	})

	t.Run("TestGetInvalidSlot", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		retrievedState := os.Get(slot)

		require.Nil(t, retrievedState)
	})

	t.Run("TestMaxSlot", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		epoch := phase0.Epoch(1)
		signerState := &SignerState{Slot: slot}

		os.Set(slot, epoch, signerState)
		require.Equal(t, os.MaxSlot(), slot)
	})

	t.Run("TestDutyCount", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		epoch := phase0.Epoch(1)
		signerState := &SignerState{Slot: slot}

		os.Set(slot, epoch, signerState)

		require.Equal(t, os.DutyCount(epoch), 1)
		require.Equal(t, os.DutyCount(epoch-1), 0)

		slot2 := phase0.Slot(6)
		epoch2 := phase0.Epoch(2)
		signerState2 := &SignerState{Slot: slot2}

		os.Set(slot2, epoch2, signerState2)

		require.Equal(t, os.DutyCount(epoch2), 1)
		require.Equal(t, os.DutyCount(epoch), 1)
		require.Equal(t, os.DutyCount(epoch-1), 0)
	})

	t.Run("TestIncrementLastEpochDuties", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		epoch := phase0.Epoch(1)
		signerState := &SignerState{Slot: slot}

		os.Set(slot, epoch, signerState)
		require.Equal(t, os.DutyCount(epoch), 1)

		slot2 := phase0.Slot(6)
		signerState2 := &SignerState{Slot: slot2}
		os.Set(slot2, epoch, signerState2)

		require.Equal(t, os.DutyCount(epoch), 2)
	})
}
