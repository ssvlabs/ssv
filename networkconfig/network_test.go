package networkconfig

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestBooleForkInPriorWindow(t *testing.T) {
	tests := []struct {
		name     string
		boole    phase0.Epoch
		epoch    phase0.Epoch
		expected bool
	}{
		{name: "before_window", boole: 10, epoch: 8, expected: false},
		{name: "in_window", boole: 10, epoch: 9, expected: true},
		{name: "at_fork", boole: 10, epoch: 10, expected: false},
		{name: "after_fork", boole: 10, epoch: 11, expected: false},
		{name: "boole_zero_epoch_zero", boole: 0, epoch: 0, expected: false},
		{name: "boole_zero_epoch_one", boole: 0, epoch: 1, expected: false},
		{name: "boole_one_epoch_zero", boole: 1, epoch: 0, expected: true},
		{name: "boole_one_epoch_one", boole: 1, epoch: 1, expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beacon := *TestNetwork.Beacon
			netCfg := Network{
				Beacon: &beacon,
				SSV:    &SSV{Forks: SSVForks{Boole: test.boole}},
			}

			slot := netCfg.FirstSlotAtEpoch(test.epoch)
			require.Equal(t, test.expected, netCfg.inBoolePriorWindow(slot))
		})
	}
}

func TestBooleForkInUnsubscriptionWindow(t *testing.T) {
	tests := []struct {
		name     string
		boole    phase0.Epoch
		slot     phase0.Slot
		expected bool
	}{
		{name: "before_fork", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10) - 1, expected: false},
		{name: "at_fork", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10), expected: true},
		{name: "after_fork_slot", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10) + 1, expected: false},
		{name: "after_fork", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(11), expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beacon := *TestNetwork.Beacon
			netCfg := Network{
				Beacon: &beacon,
				SSV:    &SSV{Forks: SSVForks{Boole: test.boole}},
			}

			require.Equal(t, test.expected, netCfg.inBooleUnsubscriptionWindow(test.slot))
		})
	}
}

func TestBooleForkAtEpoch(t *testing.T) {
	tests := []struct {
		name     string
		boole    phase0.Epoch
		epoch    phase0.Epoch
		expected bool
	}{
		{name: "before_fork", boole: 10, epoch: 9, expected: false},
		{name: "at_fork", boole: 10, epoch: 10, expected: true},
		{name: "after_fork", boole: 10, epoch: 11, expected: true},
		{name: "boole_zero_epoch_zero", boole: 0, epoch: 0, expected: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			netCfg := Network{
				SSV: &SSV{Forks: SSVForks{Boole: test.boole}},
			}

			require.Equal(t, test.expected, netCfg.BooleForkAtEpoch(test.epoch))
		})
	}
}

func TestBooleFork(t *testing.T) {
	tests := []struct {
		name           string
		currentEpoch   phase0.Epoch
		boole          phase0.Epoch
		expectedForked bool
	}{
		{name: "before_fork", currentEpoch: 2, boole: 3, expectedForked: false},
		{name: "at_fork", currentEpoch: 2, boole: 2, expectedForked: true},
		{name: "after_fork", currentEpoch: 2, boole: 1, expectedForked: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			netCfg := Network{
				Beacon: beaconAtEpoch(test.currentEpoch),
				SSV:    &SSV{Forks: SSVForks{Boole: test.boole}},
			}

			require.Equal(t, test.expectedForked, netCfg.BooleFork())
		})
	}
}

func beaconAtEpoch(epoch phase0.Epoch) *Beacon {
	beacon := *TestNetwork.Beacon
	slotsSinceGenesis := uint64(epoch) * beacon.SlotsPerEpoch
	genesisTime := time.Now().Add(-time.Duration(slotsSinceGenesis) * beacon.SlotDuration)
	beacon.GenesisTime = genesisTime
	return &beacon
}
