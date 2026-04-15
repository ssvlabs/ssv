package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func TestValidatorDutyFromDuty(t *testing.T) {
	validatorDuty := &spectypes.ValidatorDuty{}

	got, err := validatorDutyFromDuty(validatorDuty)
	require.NoError(t, err)
	require.Same(t, validatorDuty, got)

	_, err = validatorDutyFromDuty(&spectypes.CommitteeDuty{})
	require.ErrorContains(t, err, "duty is not a ValidatorDuty")
}

func TestValidatorDutyFromState(t *testing.T) {
	var runner *BaseRunner
	_, err := runner.currentValidatorDuty()
	require.ErrorContains(t, err, "runner is nil")

	_, err = (&BaseRunner{}).currentValidatorDuty()
	require.ErrorContains(t, err, "runner state is nil")

	_, err = (&BaseRunner{State: &State{}}).currentValidatorDuty()
	require.ErrorContains(t, err, "current duty is nil")

	_, err = (&BaseRunner{State: &State{CurrentDuty: &spectypes.CommitteeDuty{}}}).currentValidatorDuty()
	require.ErrorContains(t, err, "current duty: duty is not a ValidatorDuty")
}

func TestCommitteeDutyFromState(t *testing.T) {
	committeeDuty := &spectypes.CommitteeDuty{}

	got, err := (&BaseRunner{State: &State{CurrentDuty: committeeDuty}}).currentCommitteeDuty()
	require.NoError(t, err)
	require.Same(t, committeeDuty, got)

	_, err = (&BaseRunner{State: &State{CurrentDuty: &spectypes.ValidatorDuty{}}}).currentCommitteeDuty()
	require.ErrorContains(t, err, "current duty: duty is not a CommitteeDuty")
}

func TestCalculateVoluntaryExitHandlesMissingCurrentDuty(t *testing.T) {
	runner := &VoluntaryExitRunner{BaseRunner: &BaseRunner{State: &State{}}}

	require.NotPanics(t, func() {
		_, err := runner.calculateVoluntaryExit()
		require.ErrorContains(t, err, "current validator duty: current duty is nil")
	})
}

func TestDecidedValueTypeAssertions(t *testing.T) {
	consensusData := &spectypes.ValidatorConsensusData{}
	gotConsensusData, err := validatorConsensusDataFromEncoder(consensusData)
	require.NoError(t, err)
	require.Same(t, consensusData, gotConsensusData)

	beaconVote := &spectypes.BeaconVote{}
	gotBeaconVote, err := beaconVoteFromEncoder(beaconVote)
	require.NoError(t, err)
	require.Same(t, beaconVote, gotBeaconVote)

	_, err = validatorConsensusDataFromEncoder(beaconVote)
	require.ErrorContains(t, err, "decided value is not a ValidatorConsensusData")

	_, err = beaconVoteFromEncoder(consensusData)
	require.ErrorContains(t, err, "decided value is not a BeaconVote")
}
