package testenv

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/networkconfig"

	"github.com/ssvlabs/ssv/ssvsigner/e2e/common"
)

// setupMockBeacon initializes the mock beacon controller and sets up basic expectations
func (env *TestEnvironment) setupMockBeacon(t gomock.TestReporter) error {
	env.mockController = gomock.NewController(t)
	env.mockBeacon = networkconfig.NewMockBeacon(env.mockController)

	// (EstimatedCurrentSlot left for individual tests to define)
	env.setupBasicMockBeaconExpectations()

	return nil
}

// setupBasicMockBeaconExpectations configures the mock beacon with real network values
// EstimatedCurrentSlot() is intentionally left for individual tests to define
func (env *TestEnvironment) setupBasicMockBeaconExpectations() {
	realConfig := common.MainnetBeaconConfig

	env.mockBeacon.EXPECT().GetGenesisValidatorsRoot().Return(realConfig.GetGenesisValidatorsRoot()).AnyTimes()
	env.mockBeacon.EXPECT().GetNetworkName().Return(realConfig.GetNetworkName()).AnyTimes()
	env.mockBeacon.EXPECT().GetSlotsPerEpoch().Return(realConfig.GetSlotsPerEpoch()).AnyTimes()
	env.mockBeacon.EXPECT().GetSlotDuration().Return(realConfig.GetSlotDuration()).AnyTimes()
	env.mockBeacon.EXPECT().GetGenesisTime().Return(realConfig.GetGenesisTime()).AnyTimes()

	env.mockBeacon.EXPECT().BeaconForkAtEpoch(gomock.Any()).DoAndReturn(func(epoch phase0.Epoch) (spec.DataVersion, *phase0.Fork) {
		return realConfig.BeaconForkAtEpoch(epoch)
	}).AnyTimes()

	env.mockBeacon.EXPECT().EstimatedEpochAtSlot(gomock.Any()).DoAndReturn(func(slot phase0.Slot) phase0.Epoch {
		return realConfig.EstimatedEpochAtSlot(slot)
	}).AnyTimes()

	env.mockBeacon.EXPECT().GetEpochFirstSlot(gomock.Any()).DoAndReturn(func(epoch phase0.Epoch) phase0.Slot {
		return realConfig.GetEpochFirstSlot(epoch)
	}).AnyTimes()

	env.mockBeacon.EXPECT().EstimatedSlotAtTime(gomock.Any()).DoAndReturn(func(t time.Time) phase0.Slot {
		return realConfig.EstimatedSlotAtTime(t)
	}).AnyTimes()

	// NOTE: EstimatedCurrentSlot() is intentionally left for individual tests to define
}

// SetTestCurrentEpoch sets up the mock beacon to return a specific current epoch for testing
// It calculates the current slot as the middle of the epoch (first slot + 16)
func (env *TestEnvironment) SetTestCurrentEpoch(testCurrentEpoch phase0.Epoch) {
	testCurrentSlot := env.mockBeacon.GetEpochFirstSlot(testCurrentEpoch) + 16 // Middle of test current epoch
	env.mockBeacon.EXPECT().EstimatedCurrentSlot().Return(testCurrentSlot).AnyTimes()
}
