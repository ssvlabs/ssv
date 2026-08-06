package networkconfig

import (
	"math"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestBooleForkInPriorWindow(t *testing.T) {
	tests := []struct {
		name         string
		boole        phase0.Epoch
		epoch        phase0.Epoch
		windowEpochs *phase0.Epoch
		expected     bool
	}{
		{name: "before_window", boole: 10, epoch: 8, expected: false},
		{name: "in_window", boole: 10, epoch: 9, expected: true},
		{name: "at_fork", boole: 10, epoch: 10, expected: false},
		{name: "after_fork", boole: 10, epoch: 11, expected: false},
		{name: "boole_zero_epoch_zero", boole: 0, epoch: 0, expected: false},
		{name: "boole_zero_epoch_one", boole: 0, epoch: 1, expected: false},
		{name: "boole_one_epoch_zero", boole: 1, epoch: 0, expected: true},
		{name: "boole_one_epoch_one", boole: 1, epoch: 1, expected: false},
		{name: "zero_window", boole: 10, epoch: 9, windowEpochs: ptr(phase0.Epoch(0)), expected: false},
		{name: "window_larger_than_boole", boole: 1, epoch: 0, windowEpochs: ptr(phase0.Epoch(2)), expected: true},
		{name: "boole_max_epoch", boole: phase0.Epoch(math.MaxUint64), epoch: 0, windowEpochs: ptr(phase0.Epoch(1)), expected: false},
		{name: "boole_zero_epoch", boole: 0, epoch: 0, windowEpochs: ptr(phase0.Epoch(1)), expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beacon := *TestNetwork.Beacon
			netCfg := Network{
				Beacon: &beacon,
				SSV:    &SSV{Forks: SSVForks{Boole: test.boole}},
			}

			slot := netCfg.FirstSlotAtEpoch(test.epoch)
			if test.windowEpochs == nil {
				require.Equal(t, test.expected, netCfg.inBoolePriorWindow(slot))
				return
			}
			require.Equal(t, test.expected, netCfg.inBoolePriorWindowWithEpochs(slot, *test.windowEpochs))
		})
	}
}

func TestBooleForkInSubsequentWindow(t *testing.T) {
	maxEpochForTwoSlots := phase0.Epoch(math.MaxUint64 / 2)
	tests := []struct {
		name          string
		boole         phase0.Epoch
		slot          phase0.Slot
		windowSlots   *phase0.Slot
		slotsPerEpoch *uint64
		expected      bool
	}{
		// The default window keeps Alan accepted for maxStoredSlots (SlotsPerEpoch + LateSlotAllowance
		// = 32 + 2 = 34) slots after the fork, so late pre-fork-slot messages are not dropped. Window
		// is [F, F+34).
		{name: "before_fork", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10) - 1, expected: false},
		{name: "at_fork", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10), expected: true},
		{name: "one_slot_after_fork", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10) + 1, expected: true},
		{name: "one_epoch_after_fork", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(11), expected: true},
		{name: "last_slot_in_window", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10) + 33, expected: true},
		{name: "first_slot_after_window", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10) + 34, expected: false},
		{name: "boole_zero_epoch_zero", boole: 0, slot: TestNetwork.FirstSlotAtEpoch(0), expected: false},
		{name: "boole_max_epoch_zero", boole: phase0.Epoch(math.MaxUint64), slot: TestNetwork.FirstSlotAtEpoch(0), expected: false},
		{name: "zero_window", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10), windowSlots: ptr(phase0.Slot(0)), expected: false},
		{name: "boole_over_max_epoch_slot_zero", boole: maxEpochForTwoSlots + 1, slot: TestNetwork.FirstSlotAtEpoch(0), slotsPerEpoch: ptr(uint64(2)), windowSlots: ptr(phase0.Slot(1)), expected: false},
		{name: "boole_over_max_epoch", boole: maxEpochForTwoSlots + 1, slot: phase0.Slot(math.MaxUint64), slotsPerEpoch: ptr(uint64(2)), windowSlots: ptr(phase0.Slot(1)), expected: false},
		{name: "start_overflow", boole: 1, slot: phase0.Slot(math.MaxUint64), slotsPerEpoch: ptr(uint64(math.MaxUint64)), windowSlots: ptr(phase0.Slot(1)), expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beacon := *TestNetwork.Beacon
			if test.slotsPerEpoch != nil {
				beacon.SlotsPerEpoch = *test.slotsPerEpoch
			}
			netCfg := Network{
				Beacon: &beacon,
				SSV:    &SSV{Forks: SSVForks{Boole: test.boole}},
			}

			if test.windowSlots == nil {
				require.Equal(t, test.expected, netCfg.inBooleSubsequentWindow(test.slot))
				return
			}
			require.Equal(t, test.expected, netCfg.inBooleSubsequentWindowWithSlots(test.slot, *test.windowSlots))
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

func TestDomainTypeAtSlot(t *testing.T) {
	domainAlan := spectypes.DomainType{0x01, 0x02, 0x03, 0x04}
	domainBoole := spectypes.DomainType{0x05, 0x06, 0x07, 0x08}

	tests := []struct {
		name            string
		booleEpoch      phase0.Epoch
		nextDomainType  spectypes.DomainType
		slot            phase0.Slot
		expectedCurrent spectypes.DomainType
		expectedNext    spectypes.DomainType
	}{
		{
			name:            "pre_fork_uses_alan",
			booleEpoch:      10,
			nextDomainType:  domainBoole,
			slot:            TestNetwork.FirstSlotAtEpoch(8),
			expectedCurrent: domainAlan,
			expectedNext:    domainBoole,
		},
		{
			name:            "pre_fork_near_fork_uses_next_boole",
			booleEpoch:      10,
			nextDomainType:  domainBoole,
			slot:            TestNetwork.FirstSlotAtEpoch(9),
			expectedCurrent: domainAlan,
			expectedNext:    domainBoole,
		},
		{
			name:            "post_fork_uses_boole",
			booleEpoch:      10,
			nextDomainType:  domainBoole,
			slot:            TestNetwork.FirstSlotAtEpoch(10),
			expectedCurrent: domainBoole,
			expectedNext:    domainBoole,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			netCfg := Network{
				Beacon: TestNetwork.Beacon,
				SSV: &SSV{
					DomainType:     domainAlan,
					NextDomainType: test.nextDomainType,
					Forks:          SSVForks{Boole: test.booleEpoch},
				},
			}

			require.Equal(t, test.expectedCurrent, netCfg.DomainTypeAtSlot(test.slot))
			require.Equal(t, test.expectedNext, netCfg.NextDomainType)
		})
	}
}

func TestBooleForkAtSlot(t *testing.T) {
	tests := []struct {
		name     string
		boole    phase0.Epoch
		slot     phase0.Slot
		expected bool
	}{
		{name: "before_fork", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10) - 1, expected: false},
		{name: "at_fork", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10), expected: true},
		{name: "after_fork_slot", boole: 10, slot: TestNetwork.FirstSlotAtEpoch(10) + 1, expected: true},
		{name: "boole_zero_slot_zero", boole: 0, slot: TestNetwork.FirstSlotAtEpoch(0), expected: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beacon := *TestNetwork.Beacon
			netCfg := Network{
				Beacon: &beacon,
				SSV:    &SSV{Forks: SSVForks{Boole: test.boole}},
			}

			require.Equal(t, test.expected, netCfg.BooleForkAtSlot(test.slot))
		})
	}
}

func TestNetworkValidate(t *testing.T) {
	domainAlan := spectypes.DomainType{0x01, 0x02, 0x03, 0x04}
	domainBoole := spectypes.DomainType{0x05, 0x06, 0x07, 0x08}

	tests := []struct {
		name             string
		boole            phase0.Epoch
		currentEpoch     phase0.Epoch // wall-clock epoch the config is validated at
		preGenesis       bool         // put GenesisTime in the future (overrides currentEpoch)
		slotsPerEpoch    uint64
		domainType       spectypes.DomainType
		nextDomainType   spectypes.DomainType
		expectErr        bool
		expectedWarnings int
	}{
		{
			name:           "hard_error_zero_slots_per_epoch",
			boole:          10,
			slotsPerEpoch:  0,
			domainType:     domainAlan,
			nextDomainType: domainBoole,
			expectErr:      true,
		},
		{
			name:           "hard_error_zero_slots_per_epoch_unscheduled",
			boole:          phase0.Epoch(math.MaxUint64),
			slotsPerEpoch:  0,
			domainType:     domainAlan,
			nextDomainType: domainAlan,
			expectErr:      true,
		},
		{
			name:           "hard_error_overflow",
			boole:          phase0.Epoch(math.MaxUint64/32) + 1,
			slotsPerEpoch:  32,
			domainType:     domainAlan,
			nextDomainType: domainBoole,
			expectErr:      true,
		},
		{
			name:           "hard_error_next_domain_equals_domain_before_fork",
			boole:          10,
			currentEpoch:   5,
			slotsPerEpoch:  32,
			domainType:     domainAlan,
			nextDomainType: domainAlan,
			expectErr:      true,
		},
		{
			// The unchanged-domain check must not panic on EstimatedCurrentSlot when the
			// clock is still before genesis; pre-genesis the fork is not yet active, so the
			// misconfiguration is still reported as an error.
			name:           "hard_error_next_domain_equals_domain_pre_genesis",
			boole:          10,
			preGenesis:     true,
			slotsPerEpoch:  32,
			domainType:     domainAlan,
			nextDomainType: domainAlan,
			expectErr:      true,
		},
		{
			name:           "clean_next_domain_equals_domain_after_fork",
			boole:          10,
			currentEpoch:   20,
			slotsPerEpoch:  32,
			domainType:     domainBoole,
			nextDomainType: domainBoole,
		},
		{
			name:           "clean_scheduled",
			boole:          10,
			slotsPerEpoch:  32,
			domainType:     domainAlan,
			nextDomainType: domainBoole,
		},
		{
			name:           "clean_genesis_fork",
			boole:          0,
			slotsPerEpoch:  32,
			domainType:     domainAlan,
			nextDomainType: domainBoole,
		},
		{
			name:           "clean_at_max_epoch_boundary",
			boole:          phase0.Epoch(math.MaxUint64 / 32),
			slotsPerEpoch:  32,
			domainType:     domainAlan,
			nextDomainType: domainBoole,
		},
		{
			name:           "clean_unscheduled",
			boole:          phase0.Epoch(math.MaxUint64),
			slotsPerEpoch:  32,
			domainType:     domainAlan,
			nextDomainType: domainAlan,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beacon := *TestNetwork.Beacon
			beacon.SlotsPerEpoch = test.slotsPerEpoch
			slotsSinceGenesis := uint64(test.currentEpoch) * test.slotsPerEpoch
			beacon.GenesisTime = time.Now().Add(-time.Duration(slotsSinceGenesis) * beacon.SlotDuration)
			if test.preGenesis {
				beacon.GenesisTime = time.Now().Add(time.Hour)
			}
			netCfg := Network{
				Beacon: &beacon,
				SSV: &SSV{
					DomainType:     test.domainType,
					NextDomainType: test.nextDomainType,
					Forks:          SSVForks{Boole: test.boole},
				},
			}

			warnings, err := netCfg.Validate()
			if test.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, warnings, test.expectedWarnings)
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

func ptr[T any](v T) *T {
	return &v
}
