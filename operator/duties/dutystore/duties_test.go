package dutystore

import (
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDutiesSetAndQuery(t *testing.T) {
	duties := NewDuties[eth2apiv1.AttesterDuty]()
	epoch := phase0.Epoch(8)
	slot := phase0.Slot(64)

	duty := &eth2apiv1.AttesterDuty{Slot: slot, ValidatorIndex: 1}
	duties.Set(epoch, []StoreDuty[eth2apiv1.AttesterDuty]{
		{
			Slot:           slot,
			ValidatorIndex: 1,
			Duty:           duty,
			InCommittee:    true,
		},
		{
			Slot:           slot,
			ValidatorIndex: 2,
			Duty:           &eth2apiv1.AttesterDuty{Slot: slot, ValidatorIndex: 2},
			InCommittee:    false,
		},
	})

	require.True(t, duties.IsEpochSet(epoch))

	committee := duties.CommitteeSlotDuties(epoch, slot)
	require.Len(t, committee, 1)
	assert.Equal(t, duty, committee[0])

	fetched := duties.ValidatorDuty(epoch, slot, 1)
	assert.Equal(t, duty, fetched)

	indices := duties.SlotIndices(epoch, slot)
	assert.ElementsMatch(t, []phase0.ValidatorIndex{1, 2}, indices)
}

func TestDutiesResetEpoch(t *testing.T) {
	duties := NewDuties[eth2apiv1.ProposerDuty]()
	epoch := phase0.Epoch(1)
	duties.Set(epoch, []StoreDuty[eth2apiv1.ProposerDuty]{
		{Slot: 10, ValidatorIndex: 1, Duty: &eth2apiv1.ProposerDuty{}},
	})

	duties.ResetEpoch(epoch)
	assert.False(t, duties.IsEpochSet(epoch))
	assert.Nil(t, duties.CommitteeSlotDuties(epoch, 10))
	assert.Nil(t, duties.ValidatorDuty(epoch, 10, 1))
	assert.Nil(t, duties.SlotIndices(epoch, 10))
}
