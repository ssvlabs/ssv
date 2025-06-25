package testenv

import (
	"context"

	eth2api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/mocks"
	"github.com/stretchr/testify/mock"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/networkconfig"

	"github.com/ssvlabs/ssv/ssvsigner/e2e/common"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// MockConsensusClient wraps ekm.MockConsensusClient to provide dynamic behavior
type MockConsensusClient struct {
	ekm.MockConsensusClient
}

// ForkAtEpoch dynamically calls common.ForkAtEpoch
func (d *MockConsensusClient) ForkAtEpoch(ctx context.Context, epoch phase0.Epoch) (*phase0.Fork, error) {
	// Record the call for assertion purposes
	d.Called(ctx, epoch)

	// Call the actual function dynamically with the real epoch parameter
	fork, err := common.ForkAtEpoch(epoch)

	// Return the dynamic result based on the actual epoch
	return fork, err
}

// Genesis dynamically calls common.Genesis
func (d *MockConsensusClient) Genesis(ctx context.Context) (*eth2api.Genesis, error) {
	// Record the call for assertion purposes
	d.Called(ctx)

	// Call the actual function dynamically
	genesis, err := common.Genesis()

	return genesis, err
}

func (env *TestEnvironment) setupMockConsensusClient() error {
	env.mockConsensusClient = &MockConsensusClient{}

	env.mockConsensusClient.On("ForkAtEpoch", mock.Anything, mock.AnythingOfType("phase0.Epoch"))
	env.mockConsensusClient.On("Genesis", mock.Anything)

	return nil
}

// setupMockBeacon initializes the mock beacon controller and sets up basic expectations
func (env *TestEnvironment) setupMockBeacon(t gomock.TestReporter) error {
	env.mockController = gomock.NewController(t)

	// Use real config but replace beacon with our mock
	env.mockBeacon = mocks.NewMockBeaconNetwork(env.mockController)
	env.mockNetworkConfig = networkconfig.Mainnet
	env.mockNetworkConfig.Beacon = env.mockBeacon

	// (EstimatedCurrentSlot left for individual tests to define)
	env.setupBasicMockBeaconExpectations()

	return nil
}

// setupBasicMockBeaconExpectations configures the mock beacon with real network values
// EstimatedCurrentSlot() is intentionally left for individual tests to define
func (env *TestEnvironment) setupBasicMockBeaconExpectations() {
	env.mockBeacon.EXPECT().GetBeaconNetwork().Return(networkconfig.Mainnet.Beacon.GetBeaconNetwork()).AnyTimes()

	env.mockBeacon.EXPECT().EstimatedEpochAtSlot(gomock.Any()).DoAndReturn(func(slot phase0.Slot) phase0.Epoch {
		return networkconfig.Mainnet.Beacon.EstimatedEpochAtSlot(slot)
	}).AnyTimes()

	env.mockBeacon.EXPECT().GetEpochFirstSlot(gomock.Any()).DoAndReturn(func(epoch phase0.Epoch) phase0.Slot {
		return networkconfig.Mainnet.Beacon.GetEpochFirstSlot(epoch)
	}).AnyTimes()

	// NOTE: EstimatedCurrentSlot() is intentionally left for individual tests to define
}

// SetTestCurrentEpoch sets up the mock beacon to return a specific current epoch for testing
// It calculates the current slot as the middle of the epoch (first slot + 16)
func (env *TestEnvironment) SetTestCurrentEpoch(testCurrentEpoch phase0.Epoch) {
	testCurrentSlot := env.mockBeacon.GetEpochFirstSlot(testCurrentEpoch) + 16 // Middle of test current epoch
	env.mockBeacon.EXPECT().EstimatedCurrentSlot().Return(testCurrentSlot).AnyTimes()
}
