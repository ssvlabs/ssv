package doppelganger

import (
	"context"
	"testing"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
)

func newTestDoppelgangerHandler(t *testing.T) *handler {
	ctrl := gomock.NewController(t)
	mockBeaconNode := NewMockBeaconNode(ctrl)
	mockValidatorProvider := NewMockValidatorProvider(ctrl)
	logger := logging.TestLogger(t)

	return NewHandler(&Options{
		Network:           networkconfig.TestNetwork,
		BeaconNode:        mockBeaconNode,
		ValidatorProvider: mockValidatorProvider,
		Logger:            logger,
	})
}

func TestValidatorStatus(t *testing.T) {
	dg := newTestDoppelgangerHandler(t)

	// Add a validator with remaining epochs
	dg.validatorsState[1] = &doppelgangerState{remainingEpochs: 1}
	dg.validatorsState[2] = &doppelgangerState{remainingEpochs: 0}

	require.Equal(t, false, dg.CanSign(1))
	require.Equal(t, true, dg.CanSign(2))
}

func TestMarkAsSafe(t *testing.T) {
	dg := newTestDoppelgangerHandler(t)

	dg.validatorsState[1] = &doppelgangerState{remainingEpochs: 2}
	dg.ReportQuorum(1)
	require.Contains(t, dg.validatorsState, phase0.ValidatorIndex(1))
	require.Equal(t, phase0.Epoch(2), dg.validatorsState[1].remainingEpochs)
	require.Equal(t, true, dg.validatorsState[1].observedQuorum)
	require.True(t, dg.CanSign(1))
	require.True(t, dg.validatorsState[1].safe())
}

func TestUpdateDoppelgangerState(t *testing.T) {
	dg := newTestDoppelgangerHandler(t)

	// Update state with validator indices
	dg.updateDoppelgangerState(0, []phase0.ValidatorIndex{1, 2})
	require.Contains(t, dg.validatorsState, phase0.ValidatorIndex(1))
	require.Contains(t, dg.validatorsState, phase0.ValidatorIndex(2))

	dg.updateDoppelgangerState(0, []phase0.ValidatorIndex{1})
	require.Contains(t, dg.validatorsState, phase0.ValidatorIndex(1))
	require.NotContains(t, dg.validatorsState, phase0.ValidatorIndex(2))
	require.NotContains(t, dg.validatorsState, phase0.ValidatorIndex(3))
}

func TestCheckLiveness(t *testing.T) {
	dg := newTestDoppelgangerHandler(t)

	// Prepare doppelganger state
	dg.validatorsState[1] = &doppelgangerState{remainingEpochs: 1}
	dg.validatorsState[2] = &doppelgangerState{remainingEpochs: 1}

	dg.beaconNode.(*MockBeaconNode).EXPECT().ValidatorLiveness(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		[]*v1.ValidatorLiveness{
			{Index: 1, IsLive: true},
			{Index: 2, IsLive: false},
		}, nil,
	)

	dg.checkLiveness(context.Background(), 0, 0)

	require.Equal(t, initialRemainingDetectionEpochs, dg.validatorsState[1].remainingEpochs)
	require.Equal(t, phase0.Epoch(0), dg.validatorsState[2].remainingEpochs)

	require.False(t, dg.validatorsState[1].safe())
	require.True(t, dg.validatorsState[2].safe())

	require.Equal(t, false, dg.CanSign(1))
	require.Equal(t, true, dg.CanSign(2))
}

func TestProcessLivenessData(t *testing.T) {
	dg := newTestDoppelgangerHandler(t)

	dg.validatorsState[1] = &doppelgangerState{remainingEpochs: 2}
	dg.validatorsState[2] = &doppelgangerState{remainingEpochs: 2}

	dg.processLivenessData(0, []*v1.ValidatorLiveness{
		{Index: 1, IsLive: true},
		{Index: 2, IsLive: false},
	})

	require.Equal(t, initialRemainingDetectionEpochs, dg.validatorsState[1].remainingEpochs)
	require.Equal(t, phase0.Epoch(1), dg.validatorsState[2].remainingEpochs)

	require.False(t, dg.validatorsState[1].safe())
	require.False(t, dg.validatorsState[2].safe())

	require.Equal(t, false, dg.CanSign(1))
	require.Equal(t, false, dg.CanSign(2))

	dg.processLivenessData(1, []*v1.ValidatorLiveness{
		{Index: 1, IsLive: false},
		{Index: 2, IsLive: false},
	})

	require.Equal(t, phase0.Epoch(1), dg.validatorsState[1].remainingEpochs)
	require.Equal(t, phase0.Epoch(0), dg.validatorsState[2].remainingEpochs)

	require.False(t, dg.validatorsState[1].safe())
	require.True(t, dg.validatorsState[2].safe())

	require.Equal(t, false, dg.CanSign(1))
	require.Equal(t, true, dg.CanSign(2))
}

func TestRemoveValidatorState(t *testing.T) {
	dg := newTestDoppelgangerHandler(t)

	validatorIndex := phase0.ValidatorIndex(123)

	// Add the validator to the state
	dg.validatorsState[validatorIndex] = &doppelgangerState{
		remainingEpochs: 1,
	}

	// Verify the validator is initially present
	_, exists := dg.validatorsState[validatorIndex]
	require.True(t, exists, "Validator should be present before removal")

	// Remove the validator
	dg.RemoveValidatorState(validatorIndex)

	// Verify the validator is no longer in the state
	_, exists = dg.validatorsState[validatorIndex]
	require.False(t, exists, "Validator should be removed from state")

	// Try to remove a non-existent validator and ensure no panic or error occurs
	require.NotPanics(t, func() {
		dg.RemoveValidatorState(phase0.ValidatorIndex(456))
	}, "Removing a non-existent validator should not panic")
}

func TestEpochSkipReset(t *testing.T) {
	dg := newTestDoppelgangerHandler(t)

	// Add test validator states with non-default remainingEpochs
	dg.validatorsState[1] = &doppelgangerState{remainingEpochs: 5}
	dg.validatorsState[2] = &doppelgangerState{remainingEpochs: 3}

	// Call the reset method directly
	dg.resetDoppelgangerStates()

	// Verify that all validators have been reset
	for _, state := range dg.validatorsState {
		require.Equal(t, initialRemainingDetectionEpochs, state.remainingEpochs)
	}
}

func TestDecreaseRemainingEpochs_ErrorOnZero(t *testing.T) {
	state := &doppelgangerState{remainingEpochs: 0}

	err := state.decreaseRemainingEpochs()

	require.Error(t, err, "Expected error when attempting to decrease remaining epochs at 0")
	require.Equal(t, phase0.Epoch(0), state.remainingEpochs, "remainingEpochs should still be 0")
}
