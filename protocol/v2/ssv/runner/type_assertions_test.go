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
	var runner *BaseRunner
	_, err := runner.currentValidatorDuty()
	require.ErrorContains(t, err, "runner is nil")

	_, err = (&BaseRunner{}).currentValidatorDuty()
	require.ErrorContains(t, err, "runner state is nil")

	_, err = (&BaseRunner{State: &State{}}).currentValidatorDuty()
	require.ErrorContains(t, err, "current duty is nil")

	typedNilRunner := &BaseRunner{State: &State{CurrentDuty: (*spectypes.ValidatorDuty)(nil)}}
	_, err = typedNilRunner.currentValidatorDuty()
	require.ErrorContains(t, err, "validator duty is nil")

	_, err = (&BaseRunner{State: &State{CurrentDuty: &spectypes.CommitteeDuty{}}}).currentValidatorDuty()
	require.ErrorContains(t, err, "duty is not a ValidatorDuty")
}

func TestCurrentDutySlot(t *testing.T) {
	var runner *BaseRunner
	_, err := runner.currentDutySlot()
	require.ErrorContains(t, err, "runner is nil")

	_, err = (&BaseRunner{}).currentDutySlot()
	require.ErrorContains(t, err, "runner state is nil")

	_, err = (&BaseRunner{State: &State{}}).currentDutySlot()
	require.ErrorContains(t, err, "current duty is nil")

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
