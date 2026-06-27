package dutystore

import (
	"fmt"
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

func TestDutiesEraseBefore(t *testing.T) {
	duties := NewDuties[eth2apiv1.ProposerDuty]()
	for _, epoch := range []phase0.Epoch{4, 5, 6} {
		duties.Set(epoch, []StoreDuty[eth2apiv1.ProposerDuty]{
			{Slot: 10, ValidatorIndex: 1, Duty: &eth2apiv1.ProposerDuty{}},
		})
	}

	duties.EraseBefore(5)

	assert.False(t, duties.IsEpochSet(4))
	assert.True(t, duties.IsEpochSet(5))
	assert.True(t, duties.IsEpochSet(6))
}

func TestDutiesClear(t *testing.T) {
	duties := NewDuties[eth2apiv1.ProposerDuty]()
	for _, epoch := range []phase0.Epoch{4, 5, 6} {
		duties.Set(epoch, []StoreDuty[eth2apiv1.ProposerDuty]{
			{Slot: 10, ValidatorIndex: 1, Duty: &eth2apiv1.ProposerDuty{}},
		})
	}

	duties.Clear()

	for _, epoch := range []phase0.Epoch{4, 5, 6} {
		assert.False(t, duties.IsEpochSet(epoch))
	}
}

func TestStoreDutyTypesUseIndependentLocks(t *testing.T) {
	epoch := phase0.Epoch(8)
	slot := phase0.Slot(64)
	period := uint64(2)
	pk := phase0.BLSPubKey{1}

	type dutyType struct {
		name       string
		lock       func(store *Store)
		unlock     func(store *Store)
		assertOpen func(store *Store) error
	}

	newStoreWithDuties := func() *Store {
		store := New()
		store.Attester.Set(epoch, []StoreDuty[eth2apiv1.AttesterDuty]{
			{
				Slot:           slot,
				ValidatorIndex: 1,
				Duty:           &eth2apiv1.AttesterDuty{Slot: slot, ValidatorIndex: 1},
				InCommittee:    true,
			},
		})
		store.Proposer.Set(epoch, []StoreDuty[eth2apiv1.ProposerDuty]{
			{
				Slot:           slot,
				ValidatorIndex: 2,
				Duty:           &eth2apiv1.ProposerDuty{Slot: slot, ValidatorIndex: 2},
				InCommittee:    true,
			},
		})
		store.SyncCommittee.Set(period, []StoreSyncCommitteeDuty{
			{
				ValidatorIndex: 3,
				Duty:           &eth2apiv1.SyncCommitteeDuty{ValidatorIndex: 3},
				InCommittee:    true,
			},
		})
		store.VoluntaryExit.AddDuty(slot, pk)

		return store
	}

	dutyTypes := []dutyType{
		{
			name: "attester",
			lock: func(store *Store) {
				store.Attester.mu.Lock()
			},
			unlock: func(store *Store) {
				store.Attester.mu.Unlock()
			},
			assertOpen: func(store *Store) error {
				if store.Attester.ValidatorDuty(epoch, slot, 1) == nil {
					return fmt.Errorf("expected attester duty for epoch %d slot %d validator 1", epoch, slot)
				}
				return nil
			},
		},
		{
			name: "proposer",
			lock: func(store *Store) {
				store.Proposer.mu.Lock()
			},
			unlock: func(store *Store) {
				store.Proposer.mu.Unlock()
			},
			assertOpen: func(store *Store) error {
				if store.Proposer.ValidatorDuty(epoch, slot, 2) == nil {
					return fmt.Errorf("expected proposer duty for epoch %d slot %d validator 2", epoch, slot)
				}
				return nil
			},
		},
		{
			name: "sync committee",
			lock: func(store *Store) {
				store.SyncCommittee.mu.Lock()
			},
			unlock: func(store *Store) {
				store.SyncCommittee.mu.Unlock()
			},
			assertOpen: func(store *Store) error {
				if store.SyncCommittee.Duty(period, 3) == nil {
					return fmt.Errorf("expected sync committee duty for period %d validator 3", period)
				}
				return nil
			},
		},
		{
			name: "voluntary exit",
			lock: func(store *Store) {
				store.VoluntaryExit.mu.Lock()
			},
			unlock: func(store *Store) {
				store.VoluntaryExit.mu.Unlock()
			},
			assertOpen: func(store *Store) error {
				if count := store.VoluntaryExit.GetDutyCount(slot, pk); count != 1 {
					return fmt.Errorf("expected voluntary exit count 1, got %d", count)
				}
				return nil
			},
		},
	}

	for _, holder := range dutyTypes {
		t.Run(holder.name+" lock", func(t *testing.T) {
			store := newStoreWithDuties()
			holder.lock(store)
			defer holder.unlock(store)

			for _, other := range dutyTypes {
				if other.name == holder.name {
					continue
				}
				assertCompletesWithin(t, other.name+" remains available", func() error {
					return other.assertOpen(store)
				})
			}
		})
	}
}

func assertCompletesWithin(t *testing.T, name string, fn func() error) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		require.NoError(t, err, name)
	case <-time.After(time.Second):
		t.Fatalf("%s blocked while a different duty type lock was held", name)
	}
}
