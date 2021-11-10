package v1

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestForkV1_SlotTick(t *testing.T) {
	v1Fork := New(100)

	t.Run("initial value", func(t *testing.T) {
		require.Equal(t, uint64(0), v1Fork.(*ForkV1).currentSlot.Get())
	})

	t.Run("setting", func(t *testing.T) {
		v1Fork.SlotTick(99)
		require.EqualValues(t, uint64(99), v1Fork.(*ForkV1).currentSlot.Get())
		require.False(t, v1Fork.(*ForkV1).forked())
	})

	t.Run("forked", func(t *testing.T) {
		v1Fork.SlotTick(100)
		require.EqualValues(t, uint64(100), v1Fork.(*ForkV1).currentSlot.Get())
		require.True(t, v1Fork.(*ForkV1).forked())
	})

	t.Run("forked", func(t *testing.T) {
		v1Fork.SlotTick(1000)
		require.EqualValues(t, uint64(1000), v1Fork.(*ForkV1).currentSlot.Get())
		require.True(t, v1Fork.(*ForkV1).forked())
	})
}
