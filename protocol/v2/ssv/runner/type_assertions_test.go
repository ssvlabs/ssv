package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
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

	aggregatorCommitteeDuty := &spectypes.AggregatorCommitteeDuty{
		Slot:            17,
		ValidatorDuties: []*spectypes.ValidatorDuty{{Slot: 17}},
	}
	slot, err = (&BaseRunner{State: &State{CurrentDuty: aggregatorCommitteeDuty}}).currentDutySlot()
	require.NoError(t, err)
	require.Equal(t, aggregatorCommitteeDuty.DutySlot(), slot)
}

func TestDecidedAttestationVote(t *testing.T) {
	beaconVote := &spectypes.BeaconVote{}

	gotVote, gotIndex, err := decidedAttestationVote(beaconVote)
	require.NoError(t, err)
	require.Same(t, beaconVote, gotVote)
	require.Nil(t, gotIndex) // no attestation index before Gloas

	// Gloas: the BeaconVote half is extracted and the carried attestation index is returned (non-nil).
	gloasVote := &gloas.GloasBeaconVote{
		BlockRoot:            phase0.Root{0x01},
		Source:               &phase0.Checkpoint{},
		Target:               &phase0.Checkpoint{Epoch: 1},
		AttestationDataIndex: 1,
	}
	gotVote, gotIndex, err = decidedAttestationVote(gloasVote)
	require.NoError(t, err)
	require.Equal(t, gloasVote.BlockRoot, gotVote.BlockRoot)
	require.NotNil(t, gotIndex)
	require.Equal(t, phase0.CommitteeIndex(1), *gotIndex)

	_, _, err = decidedAttestationVote(nil)
	require.ErrorContains(t, err, "decided value is nil")

	var nilBeaconVote *spectypes.BeaconVote
	_, _, err = decidedAttestationVote(nilBeaconVote)
	require.ErrorContains(t, err, "beacon vote is nil")

	var nilGloasVote *gloas.GloasBeaconVote
	_, _, err = decidedAttestationVote(nilGloasVote)
	require.ErrorContains(t, err, "gloas beacon vote is nil")

	_, _, err = decidedAttestationVote(&spectypes.ProposerConsensusData{})
	require.ErrorContains(t, err, "decided value is not a beacon vote")
}
