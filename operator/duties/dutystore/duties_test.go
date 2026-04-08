package dutystore

import (
	"testing"
	"time"

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

func TestDutiesEraseEpochData(t *testing.T) {
	duties := NewDuties[eth2apiv1.ProposerDuty]()
	epoch := phase0.Epoch(1)
	duties.Set(epoch, []StoreDuty[eth2apiv1.ProposerDuty]{
		{Slot: 10, ValidatorIndex: 1, Duty: &eth2apiv1.ProposerDuty{}},
	})

	duties.EraseEpochData(epoch)
	assert.False(t, duties.IsEpochSet(epoch))
	assert.Nil(t, duties.CommitteeSlotDuties(epoch, 10))
	assert.Nil(t, duties.ValidatorDuty(epoch, 10, 1))
	assert.Nil(t, duties.SlotIndices(epoch, 10))
}

func TestStoreDutyTypesUseIndependentLocks(t *testing.T) {
	store := New()

	epoch := phase0.Epoch(8)
	slot := phase0.Slot(64)
	period := uint64(2)
	pk := phase0.BLSPubKey{1}

	store.Proposer.Set(epoch, []StoreDuty[eth2apiv1.ProposerDuty]{
		{
			Slot:           slot,
			ValidatorIndex: 1,
			Duty:           &eth2apiv1.ProposerDuty{Slot: slot, ValidatorIndex: 1},
			InCommittee:    true,
		},
	})
	store.SyncCommittee.Set(period, []StoreSyncCommitteeDuty{
		{
			ValidatorIndex: 2,
			Duty:           &eth2apiv1.SyncCommitteeDuty{ValidatorIndex: 2},
			InCommittee:    true,
		},
	})

	// Hold the attester write lock and verify other duty types still make progress.
	store.Attester.mu.Lock()
	defer store.Attester.mu.Unlock()

	assertCompletesWithin(t, "proposer read", func() {
		assert.NotNil(t, store.Proposer.ValidatorDuty(epoch, slot, 1))
	})
	assertCompletesWithin(t, "sync committee read", func() {
		assert.NotNil(t, store.SyncCommittee.Duty(period, 2))
	})
	assertCompletesWithin(t, "voluntary exit write", func() {
		store.VoluntaryExit.AddDuty(slot, pk)
		assert.Equal(t, uint64(1), store.VoluntaryExit.GetDutyCount(slot, pk))
	})
}

func assertCompletesWithin(t *testing.T, name string, fn func()) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("%s blocked while a different duty type lock was held", name)
	}
}
