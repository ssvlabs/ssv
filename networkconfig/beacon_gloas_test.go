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

func TestBeacon_GloasForkEpoch(t *testing.T) {
	_, ok := (&Beacon{Forks: map[spec.DataVersion]phase0.Fork{}}).GloasForkEpoch()
	require.False(t, ok)

	epoch, ok := (&Beacon{Forks: map[spec.DataVersion]phase0.Fork{
		DataVersionGloas: {Epoch: 100},
	}}).GloasForkEpoch()
	require.True(t, ok)
	require.Equal(t, phase0.Epoch(100), epoch)
}

func TestNetwork_InGloasPriorWindow(t *testing.T) {
	const gloasEpoch = 100
	netCfg := TestNetworkWithGloas(gloasEpoch)
	slotInEpoch := func(e phase0.Epoch) phase0.Slot { return phase0.Slot(uint64(e) * netCfg.SlotsPerEpoch) }

	require.False(t, netCfg.InGloasPriorWindow(slotInEpoch(gloasEpoch-2)), "outside the lookahead window")
	require.True(t, netCfg.InGloasPriorWindow(slotInEpoch(gloasEpoch-1)), "the prior window")
	require.False(t, netCfg.InGloasPriorWindow(slotInEpoch(gloasEpoch)), "already at the fork")

	// No Gloas fork scheduled → never in the window.
	require.False(t, TestNetwork.InGloasPriorWindow(slotInEpoch(gloasEpoch-1)))
}

// IntervalDuration is a third of the slot before Gloas, a quarter from the fork on (SIP #94 §1).
func TestBeacon_IntervalDuration(t *testing.T) {
	// No Gloas fork → always thirds.
	require.Equal(t, TestNetwork.SlotDuration/3, TestNetwork.IntervalDuration(0))
	require.Equal(t, TestNetwork.SlotDuration/3, TestNetwork.IntervalDuration(1_000_000))

	// Gloas at epoch 100: thirds before the fork, quarters from it on.
	const gloasEpoch = 100
	netCfg := TestNetworkWithGloas(gloasEpoch)
	require.Equal(t, netCfg.SlotDuration/3, netCfg.IntervalDuration(netCfg.FirstSlotAtEpoch(gloasEpoch)-1))
	require.Equal(t, netCfg.SlotDuration/4, netCfg.IntervalDuration(netCfg.FirstSlotAtEpoch(gloasEpoch)))
}
