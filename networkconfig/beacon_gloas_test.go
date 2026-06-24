package networkconfig

import (
	"math"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestBeacon_IsGloas(t *testing.T) {
	// No Gloas entry in the fork map → never Gloas.
	none := &Beacon{Forks: map[spec.DataVersion]phase0.Fork{}}
	require.False(t, none.IsGloas(0))
	require.False(t, none.IsGloas(1_000_000))

	// Unscheduled Gloas (far-future sentinel) → never Gloas.
	farFuture := &Beacon{Forks: map[spec.DataVersion]phase0.Fork{
		DataVersionGloas: {Epoch: phase0.Epoch(math.MaxUint64)},
	}}
	require.False(t, farFuture.IsGloas(1_000_000))

	// Scheduled at epoch 100.
	scheduled := &Beacon{Forks: map[spec.DataVersion]phase0.Fork{
		DataVersionGloas: {Epoch: 100},
	}}
	require.False(t, scheduled.IsGloas(99))
	require.True(t, scheduled.IsGloas(100))
	require.True(t, scheduled.IsGloas(101))
}
