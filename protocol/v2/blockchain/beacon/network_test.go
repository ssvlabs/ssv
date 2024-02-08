package beacon

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestNewNetwork(t *testing.T) {
	parent := spectypes.BeaconNetwork("prater")
	network := NewNetwork(parent)
	require.NotNil(t, network)
	require.Equal(t, parent, network.Parent)
}

func TestNetwork_String_WithName(t *testing.T) {
	network := Network{Name: "TestNetwork"}
	require.Equal(t, "TestNetwork", network.String())
}

func TestNetwork_String_WithParent(t *testing.T) {
	network := Network{Parent: "ParentNetwork"}
	require.Equal(t, "ParentNetwork", network.String())
}

func TestNetwork_String_Default(t *testing.T) {
	network := Network{}
	require.Equal(t, "", network.String())
}

func TestNetwork_ForkVersion_Direct(t *testing.T) {
	expectedForkVersion := [4]byte{1, 2, 3, 4}
	network := Network{ForkVersionVal: expectedForkVersion}
	require.Equal(t, expectedForkVersion, network.ForkVersion())
}

func TestNetwork_ForkVersion_Parent(t *testing.T) {
	network := Network{Parent: "prater", ForkVersionVal: [4]byte{}}
	require.Equal(t, spectypes.BeaconNetwork("prater").ForkVersion(), network.ForkVersion())
}

func TestNetwork_ForkVersion_Default(t *testing.T) {
	network := Network{}
	require.Equal(t, [4]byte{}, network.ForkVersion())
}

func TestNetwork_MinGenesisTime_Direct(t *testing.T) {
	expectedMinGenesisTime := uint64(123456)
	network := Network{MinGenesisTimeVal: expectedMinGenesisTime}
	require.Equal(t, expectedMinGenesisTime, network.MinGenesisTime())
}

func TestNetwork_MinGenesisTime_Parent(t *testing.T) {
	network := Network{Parent: "prater", MinGenesisTimeVal: 0}
	require.Equal(t, spectypes.BeaconNetwork("prater").MinGenesisTime(), network.MinGenesisTime())
}

func TestNetwork_MinGenesisTime_Default(t *testing.T) {
	network := Network{}
	require.Equal(t, uint64(0), network.MinGenesisTime())
}

func TestNetwork_SlotDuration_Direct(t *testing.T) {
	expectedSlotDuration := 1 * time.Second
	network := Network{SlotDurationVal: expectedSlotDuration}
	require.Equal(t, expectedSlotDuration, network.SlotDuration())
}

func TestNetwork_SlotDuration_Parent(t *testing.T) {
	network := Network{Parent: "prater", SlotDurationVal: 0}
	require.Equal(t, spectypes.BeaconNetwork("prater").SlotDurationSec(), network.SlotDuration())
}

func TestNetwork_SlotDuration_Default(t *testing.T) {
	network := Network{}
	require.Equal(t, defaultSlotDuration, network.SlotDuration())
}

func TestNetwork_SlotsPerEpoch_Direct(t *testing.T) {
	expectedSlotsPerEpoch := uint64(1)
	network := Network{SlotsPerEpochVal: expectedSlotsPerEpoch}
	require.Equal(t, expectedSlotsPerEpoch, network.SlotsPerEpoch())
}

func TestNetwork_SlotsPerEpoch_Parent(t *testing.T) {
	network := Network{Parent: "prater", SlotsPerEpochVal: 0}
	require.Equal(t, spectypes.BeaconNetwork("prater").SlotsPerEpoch(), network.SlotsPerEpoch())
}

func TestNetwork_SlotsPerEpoch_Default(t *testing.T) {
	network := Network{}
	require.Equal(t, defaultSlotsPerEpoch, network.SlotsPerEpoch())
}

func TestNetwork_EpochsPerSyncCommitteePeriod_Direct(t *testing.T) {
	expectedEpochsPerSyncCommitteePeriod := uint64(1)
	network := Network{EpochsPerSyncCommitteePeriodVal: expectedEpochsPerSyncCommitteePeriod}
	require.Equal(t, expectedEpochsPerSyncCommitteePeriod, network.EpochsPerSyncCommitteePeriod())
}

func TestNetwork_EpochsPerSyncCommitteePeriod_Default(t *testing.T) {
	network := Network{}
	require.Equal(t, defaultEpochsPerSyncCommitteePeriod, network.EpochsPerSyncCommitteePeriod())
}

func TestNetwork_GetSlotEndTime(t *testing.T) {
	slot := phase0.Slot(1)

	n := NewNetwork(spectypes.PraterNetwork)
	slotStart := n.GetSlotStartTime(slot)
	slotEnd := n.GetSlotEndTime(slot)

	require.Equal(t, n.SlotDuration(), slotEnd.Sub(slotStart))
}
