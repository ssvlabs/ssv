package doppelganger

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
)

func newTestDoppelgangerService(t *testing.T) *DoppelgangerService {
	ctrl := gomock.NewController(t)
	mockBeaconNode := NewMockBeaconNode(ctrl)
	mockValidatorProvider := NewMockValidatorProvider(ctrl)
	logger := logging.TestLogger(t)

	service := NewDoppelgangerService(&DoppelgangerOptions{
		Network:           networkconfig.NetworkConfig{},
		BeaconNode:        mockBeaconNode,
		ValidatorProvider: mockValidatorProvider,
		//SlotTickerProvider: func() slotticker.SlotTicker { return },
		Logger: logger,
	})

	return service.(*DoppelgangerService)
}

func TestValidatorStatus(t *testing.T) {
	ds := newTestDoppelgangerService(t)

	// Add a validator with remaining epochs
	ds.doppelgangerState[1] = &DoppelgangerState{RemainingEpochs: 1}
	ds.doppelgangerState[2] = &DoppelgangerState{RemainingEpochs: 0}

	require.Equal(t, SigningDisabled, ds.ValidatorStatus(1))
	require.Equal(t, SigningEnabled, ds.ValidatorStatus(2))
	require.Equal(t, UnknownToDoppelganger, ds.ValidatorStatus(3))
}

func TestMarkAsSafe(t *testing.T) {
	ds := newTestDoppelgangerService(t)

	ds.doppelgangerState[1] = &DoppelgangerState{RemainingEpochs: 2}
	ds.MarkAsSafe(1)
	require.Contains(t, ds.doppelgangerState, phase0.ValidatorIndex(1))
	require.Equal(t, uint64(0), ds.doppelgangerState[1].RemainingEpochs)
}

func TestUpdateDoppelgangerState(t *testing.T) {
	ds := newTestDoppelgangerService(t)

	// Update state with validator indices
	ds.updateDoppelgangerState([]phase0.ValidatorIndex{1, 2})
	require.Contains(t, ds.doppelgangerState, phase0.ValidatorIndex(1))
	require.Contains(t, ds.doppelgangerState, phase0.ValidatorIndex(2))

	ds.updateDoppelgangerState([]phase0.ValidatorIndex{1})
	require.Contains(t, ds.doppelgangerState, phase0.ValidatorIndex(1))
	require.NotContains(t, ds.doppelgangerState, phase0.ValidatorIndex(2))
	require.NotContains(t, ds.doppelgangerState, phase0.ValidatorIndex(3))
}

func TestCheckLiveness(t *testing.T) {
	ds := newTestDoppelgangerService(t)

	// Prepare doppelganger state
	ds.doppelgangerState[1] = &DoppelgangerState{RemainingEpochs: 2}
	ds.doppelgangerState[2] = &DoppelgangerState{RemainingEpochs: 1}

	ds.beaconNode.(*MockBeaconNode).EXPECT().ValidatorLiveness(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		[]*v1.ValidatorLiveness{
			{Index: 1, IsLive: true},
			{Index: 2, IsLive: false},
		}, nil,
	)

	ds.checkLiveness(context.Background(), 0)

	require.Equal(t, ^uint64(0), ds.doppelgangerState[1].RemainingEpochs) // Marked as permanently unsafe
	require.Equal(t, uint64(0), ds.doppelgangerState[2].RemainingEpochs)  // Reduced to 0

	require.True(t, ds.doppelgangerState[1].requiresFurtherChecks())
	require.False(t, ds.doppelgangerState[2].requiresFurtherChecks())

	require.Equal(t, SigningDisabled, ds.ValidatorStatus(1))
	require.Equal(t, SigningEnabled, ds.ValidatorStatus(2))
}

func TestProcessLivenessData(t *testing.T) {
	ds := newTestDoppelgangerService(t)

	ds.doppelgangerState[1] = &DoppelgangerState{RemainingEpochs: 2}
	ds.doppelgangerState[2] = &DoppelgangerState{RemainingEpochs: 2}

	ds.processLivenessData(0, []*v1.ValidatorLiveness{
		{Index: 1, IsLive: true},
		{Index: 2, IsLive: false},
	})

	require.Equal(t, ^uint64(0), ds.doppelgangerState[1].RemainingEpochs)
	require.Equal(t, uint64(1), ds.doppelgangerState[2].RemainingEpochs)

	require.True(t, ds.doppelgangerState[1].requiresFurtherChecks())
	require.True(t, ds.doppelgangerState[2].requiresFurtherChecks())

	require.Equal(t, SigningDisabled, ds.ValidatorStatus(1))
	require.Equal(t, SigningDisabled, ds.ValidatorStatus(2))

	ds.processLivenessData(1, []*v1.ValidatorLiveness{
		{Index: 1, IsLive: false},
		{Index: 2, IsLive: false},
	})

	require.Equal(t, uint64(2), ds.doppelgangerState[1].RemainingEpochs)
	require.Equal(t, uint64(0), ds.doppelgangerState[2].RemainingEpochs)

	require.True(t, ds.doppelgangerState[1].requiresFurtherChecks())
	require.False(t, ds.doppelgangerState[2].requiresFurtherChecks())

	require.Equal(t, SigningDisabled, ds.ValidatorStatus(1))
	require.Equal(t, SigningEnabled, ds.ValidatorStatus(2))
}
