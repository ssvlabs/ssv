package validation

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
)

func TestOperatorState(t *testing.T) {
	t.Run("TestNewOperatorState", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)
		assert.NotNil(t, os)
		assert.Equal(t, len(os.state), int(size))
	})

	t.Run("TestGetAndSet", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		epoch := phase0.Epoch(1)
		signerState := &SignerState{Slot: slot}

		os.Set(slot, epoch, signerState)
		retrievedState := os.Get(slot)

		assert.NotNil(t, retrievedState)
		assert.Equal(t, retrievedState.Slot, slot)
	})

	t.Run("TestGetInvalidSlot", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		retrievedState := os.Get(slot)

		assert.Nil(t, retrievedState)
	})

	t.Run("TestMaxSlot", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		epoch := phase0.Epoch(1)
		signerState := &SignerState{Slot: slot}

		os.Set(slot, epoch, signerState)
		assert.Equal(t, os.MaxSlot(), slot)
	})

	t.Run("TestDutyCount", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		epoch := phase0.Epoch(1)
		signerState := &SignerState{Slot: slot}

		os.Set(slot, epoch, signerState)

		assert.Equal(t, os.DutyCount(epoch), 1)
		assert.Equal(t, os.DutyCount(epoch-1), 0)

		slot2 := phase0.Slot(6)
		epoch2 := phase0.Epoch(2)
		signerState2 := &SignerState{Slot: slot2}

		os.Set(slot2, epoch2, signerState2)

		assert.Equal(t, os.DutyCount(epoch2), 1)
		assert.Equal(t, os.DutyCount(epoch), 1)
		assert.Equal(t, os.DutyCount(epoch-1), 0)
	})

	t.Run("TestIncrementLastEpochDuties", func(t *testing.T) {
		size := phase0.Slot(10)
		os := newOperatorState(size)

		slot := phase0.Slot(5)
		epoch := phase0.Epoch(1)
		signerState := &SignerState{Slot: slot}

		// First set
		os.Set(slot, epoch, signerState)
		assert.Equal(t, os.DutyCount(epoch), 1)

		// Second set within the same epoch
		slot2 := phase0.Slot(6)
		signerState2 := &SignerState{Slot: slot2}
		os.Set(slot2, epoch, signerState2)

		assert.Equal(t, os.DutyCount(epoch), 2)
	})
}
