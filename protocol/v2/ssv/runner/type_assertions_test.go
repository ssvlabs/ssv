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

	_, err = validatorDutyFromDuty(nil)
	require.ErrorContains(t, err, "duty is nil")

	var nilValidatorDuty *spectypes.ValidatorDuty
	_, err = validatorDutyFromDuty(nilValidatorDuty)
	require.ErrorContains(t, err, "validator duty is nil")

	_, err = validatorDutyFromDuty(&spectypes.CommitteeDuty{})
	require.ErrorContains(t, err, "duty is not a ValidatorDuty")
}

func TestCurrentValidatorDuty(t *testing.T) {
	_, err := (&BaseRunner{}).currentValidatorDuty()
	require.ErrorContains(t, err, "no current duty assigned")

	typedNilRunner := &BaseRunner{State: &State{CurrentDuty: (*spectypes.ValidatorDuty)(nil)}}
	_, err = typedNilRunner.currentValidatorDuty()
	require.ErrorContains(t, err, "validator duty is nil")

	_, err = (&BaseRunner{State: &State{CurrentDuty: &spectypes.CommitteeDuty{}}}).currentValidatorDuty()
	require.ErrorContains(t, err, "duty is not a ValidatorDuty")
}

func TestCurrentCommitteeDuty(t *testing.T) {
	_, err := (&BaseRunner{}).currentCommitteeDuty()
	require.ErrorContains(t, err, "no current duty assigned")

	committeeDuty := &spectypes.CommitteeDuty{}

	got, err := (&BaseRunner{State: &State{CurrentDuty: committeeDuty}}).currentCommitteeDuty()
	require.NoError(t, err)
	require.Same(t, committeeDuty, got)

	typedNilRunner := &BaseRunner{State: &State{CurrentDuty: (*spectypes.CommitteeDuty)(nil)}}
	_, err = typedNilRunner.currentCommitteeDuty()
	require.ErrorContains(t, err, "committee duty is nil")

	_, err = (&BaseRunner{State: &State{CurrentDuty: &spectypes.ValidatorDuty{}}}).currentCommitteeDuty()
	require.ErrorContains(t, err, "duty is not a CommitteeDuty")
}

func TestCurrentDutySlot(t *testing.T) {
	_, err := (&BaseRunner{}).currentDutySlot()
	require.ErrorContains(t, err, "no current duty assigned")

	validatorDuty := &spectypes.ValidatorDuty{Slot: 11}
	slot, err := (&BaseRunner{State: &State{CurrentDuty: validatorDuty}}).currentDutySlot()
	require.NoError(t, err)
	require.Equal(t, validatorDuty.DutySlot(), slot)

	committeeDuty := &spectypes.CommitteeDuty{
		Slot:            13,
		ValidatorDuties: []*spectypes.ValidatorDuty{{Slot: 13}},
	}
	slot, err = (&BaseRunner{State: &State{CurrentDuty: committeeDuty}}).currentDutySlot()
	require.NoError(t, err)
	require.Equal(t, committeeDuty.DutySlot(), slot)
}

func TestDecidedValueTypeAssertions(t *testing.T) {
	consensusData := &spectypes.ProposerConsensusData{}
	gotConsensusData, err := validatorConsensusDataFromEncoder(consensusData)
	require.NoError(t, err)
	require.Same(t, consensusData, gotConsensusData)

	_, err = validatorConsensusDataFromEncoder(nil)
	require.ErrorContains(t, err, "decided value is nil")

	var nilConsensusData *spectypes.ProposerConsensusData
	_, err = validatorConsensusDataFromEncoder(nilConsensusData)
	require.ErrorContains(t, err, "validator consensus data is nil")

	beaconVote := &spectypes.BeaconVote{}
	gotBeaconVote, err := beaconVoteFromEncoder(beaconVote)
	require.NoError(t, err)
	require.Same(t, beaconVote, gotBeaconVote)

	_, err = beaconVoteFromEncoder(nil)
	require.ErrorContains(t, err, "decided value is nil")

	var nilBeaconVote *spectypes.BeaconVote
	_, err = beaconVoteFromEncoder(nilBeaconVote)
	require.ErrorContains(t, err, "beacon vote is nil")

	_, err = validatorConsensusDataFromEncoder(beaconVote)
	require.ErrorContains(t, err, "decided value is not a ValidatorConsensusData")

	_, err = beaconVoteFromEncoder(consensusData)
	require.ErrorContains(t, err, "decided value is not a BeaconVote")
}
