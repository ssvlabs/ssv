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

func newTestDoppelgangerHandler(t *testing.T) *doppelgangerHandler {
	ctrl := gomock.NewController(t)
	mockBeaconNode := NewMockBeaconNode(ctrl)
	mockValidatorProvider := NewMockValidatorProvider(ctrl)
	logger := logging.TestLogger(t)

	handler := NewDoppelgangerHandler(&DoppelgangerOptions{
		Network:           networkconfig.NetworkConfig{},
		BeaconNode:        mockBeaconNode,
		ValidatorProvider: mockValidatorProvider,
		Logger:            logger,
	})

	return handler.(*doppelgangerHandler)
}

func TestValidatorStatus(t *testing.T) {
	ds := newTestDoppelgangerHandler(t)

	// Add a validator with remaining epochs
	ds.doppelgangerState[1] = &doppelgangerState{remainingEpochs: 1}
	ds.doppelgangerState[2] = &doppelgangerState{remainingEpochs: 0}

	require.Equal(t, SigningDisabled, ds.ValidatorStatus(1))
	require.Equal(t, SigningEnabled, ds.ValidatorStatus(2))
	require.Equal(t, UnknownToDoppelganger, ds.ValidatorStatus(3))
}

func TestMarkAsSafe(t *testing.T) {
	ds := newTestDoppelgangerHandler(t)

	ds.doppelgangerState[1] = &doppelgangerState{remainingEpochs: 2}
	ds.MarkAsSafe(1)
	require.Contains(t, ds.doppelgangerState, phase0.ValidatorIndex(1))
	require.Equal(t, uint64(0), ds.doppelgangerState[1].remainingEpochs)
}

func TestUpdateDoppelgangerState(t *testing.T) {
	ds := newTestDoppelgangerHandler(t)

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
	ds := newTestDoppelgangerHandler(t)

	// Prepare doppelganger state
	ds.doppelgangerState[1] = &doppelgangerState{remainingEpochs: 2}
	ds.doppelgangerState[2] = &doppelgangerState{remainingEpochs: 1}

	ds.beaconNode.(*MockBeaconNode).EXPECT().ValidatorLiveness(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		[]*v1.ValidatorLiveness{
			{Index: 1, IsLive: true},
			{Index: 2, IsLive: false},
		}, nil,
	)

	ds.checkLiveness(context.Background(), 0)

	require.Equal(t, PermanentlyUnsafe, ds.doppelgangerState[1].remainingEpochs) // Marked as permanently unsafe
	require.Equal(t, uint64(0), ds.doppelgangerState[2].remainingEpochs)         // Reduced to 0

	require.True(t, ds.doppelgangerState[1].requiresFurtherChecks())
	require.False(t, ds.doppelgangerState[2].requiresFurtherChecks())

	require.Equal(t, SigningDisabled, ds.ValidatorStatus(1))
	require.Equal(t, SigningEnabled, ds.ValidatorStatus(2))
}

func TestProcessLivenessData(t *testing.T) {
	ds := newTestDoppelgangerHandler(t)

	ds.doppelgangerState[1] = &doppelgangerState{remainingEpochs: 2}
	ds.doppelgangerState[2] = &doppelgangerState{remainingEpochs: 2}

	ds.processLivenessData(0, []*v1.ValidatorLiveness{
		{Index: 1, IsLive: true},
		{Index: 2, IsLive: false},
	})

	require.Equal(t, PermanentlyUnsafe, ds.doppelgangerState[1].remainingEpochs)
	require.Equal(t, uint64(1), ds.doppelgangerState[2].remainingEpochs)

	require.True(t, ds.doppelgangerState[1].requiresFurtherChecks())
	require.True(t, ds.doppelgangerState[2].requiresFurtherChecks())

	require.Equal(t, SigningDisabled, ds.ValidatorStatus(1))
	require.Equal(t, SigningDisabled, ds.ValidatorStatus(2))

	ds.processLivenessData(1, []*v1.ValidatorLiveness{
		{Index: 1, IsLive: false},
		{Index: 2, IsLive: false},
	})

	require.Equal(t, uint64(1), ds.doppelgangerState[1].remainingEpochs)
	require.Equal(t, uint64(0), ds.doppelgangerState[2].remainingEpochs)

	require.True(t, ds.doppelgangerState[1].requiresFurtherChecks())
	require.False(t, ds.doppelgangerState[2].requiresFurtherChecks())

	require.Equal(t, SigningDisabled, ds.ValidatorStatus(1))
	require.Equal(t, SigningEnabled, ds.ValidatorStatus(2))
}

func TestRemoveValidatorState(t *testing.T) {
	ds := newTestDoppelgangerHandler(t)

	validatorIndex := phase0.ValidatorIndex(123)

	// Add the validator to the state
	ds.doppelgangerState[validatorIndex] = &doppelgangerState{
		remainingEpochs: 1,
	}

	// Verify the validator is initially present
	_, exists := ds.doppelgangerState[validatorIndex]
	require.True(t, exists, "Validator should be present before removal")

	// Remove the validator
	ds.RemoveValidatorState(validatorIndex)

	// Verify the validator is no longer in the state
	_, exists = ds.doppelgangerState[validatorIndex]
	require.False(t, exists, "Validator should be removed from state")

	// Try to remove a non-existent validator and ensure no panic or error occurs
	require.NotPanics(t, func() {
		ds.RemoveValidatorState(phase0.ValidatorIndex(456))
	}, "Removing a non-existent validator should not panic")
}
