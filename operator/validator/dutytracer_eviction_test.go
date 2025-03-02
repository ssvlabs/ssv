package validator

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/exporter/v2/store"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEvictCommitteeDuty(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)
	_, vstore, _ := registrystorage.NewSharesStorage(db, nil)

	tracer := NewTracer(context.TODO(), zap.NewNop(), vstore, nil, dutyStore, "BN")

	var committeeID1 spectypes.CommitteeID
	committeeID1[0] = 1

	var committeeID2 spectypes.CommitteeID
	committeeID2[0] = 2

	// three slots X two committees
	slot3 := phase0.Slot(3)

	dutyTrace := tracer.getOrCreateCommitteeTrace(slot3, committeeID1)
	require.NotNil(t, dutyTrace)

	dutyTrace = tracer.getOrCreateCommitteeTrace(slot3, committeeID2)
	require.NotNil(t, dutyTrace)

	slot4 := phase0.Slot(4)

	dutyTrace = tracer.getOrCreateCommitteeTrace(slot4, committeeID1)
	require.NotNil(t, dutyTrace)

	dutyTrace = tracer.getOrCreateCommitteeTrace(slot4, committeeID2)
	require.NotNil(t, dutyTrace)

	slot5 := phase0.Slot(5)

	dutyTrace = tracer.getOrCreateCommitteeTrace(slot5, committeeID1)
	require.NotNil(t, dutyTrace)

	dutyTrace = tracer.getOrCreateCommitteeTrace(slot5, committeeID2)
	require.NotNil(t, dutyTrace)

	// evict traces for slot 6 - meaning that slot 3 and 4 should be evicted
	slot6 := phase0.Slot(6)

	tracer.evictCommitteeTraces(slot6)

	var slot5count int
	tracer.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *TypedSyncMap[phase0.Slot, *committeeDutyTrace]) bool {
		slotToTraceMap.Range(func(slot phase0.Slot, dutyTrace *committeeDutyTrace) bool {
			if slot == slot5 {
				slot5count++
			}
			if slot <= slot4 {
				t.Fatalf("slot %d not evicted", slot)
			}
			return true
		})
		return true
	})

	require.Equal(t, 2, slot5count)

	storedDuty3_1, err := dutyStore.GetCommitteeDuty(slot3, committeeID1)
	require.NoError(t, err)
	require.NotNil(t, storedDuty3_1)
	assert.Equal(t, slot3, storedDuty3_1.Slot)

	storedDuty3_2, err := dutyStore.GetCommitteeDuty(slot3, committeeID2)
	require.NoError(t, err)
	require.NotNil(t, storedDuty3_2)
	assert.Equal(t, slot3, storedDuty3_2.Slot)

	storedDuty4_1, err := dutyStore.GetCommitteeDuty(slot4, committeeID1)
	require.NoError(t, err)
	require.NotNil(t, storedDuty4_1)
	assert.Equal(t, slot4, storedDuty4_1.Slot)

	storedDuty4_2, err := dutyStore.GetCommitteeDuty(slot4, committeeID2)
	require.NoError(t, err)
	require.NotNil(t, storedDuty4_2)
	assert.Equal(t, slot4, storedDuty4_2.Slot)

	storedDuty5, err := dutyStore.GetCommitteeDuty(slot5, committeeID1)
	assert.ErrorIs(t, err, store.ErrNotFound)
	require.Nil(t, storedDuty5)
}

func TestEvictValidatorDuty(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)
	_, vstore, _ := registrystorage.NewSharesStorage(db, nil)

	tracer := NewTracer(context.TODO(), zap.NewNop(), vstore, nil, dutyStore, "BN")

	var validatorPK1 spectypes.ValidatorPK
	validatorPK1[0] = 1

	var validatorPK2 spectypes.ValidatorPK
	validatorPK2[0] = 2

	slot3 := phase0.Slot(3)

	dutyTrace, mod := tracer.getOrCreateValidatorTrace(slot3, spectypes.BNRoleProposer, validatorPK1)
	mod.Validator = phase0.ValidatorIndex(1)
	require.NotNil(t, dutyTrace)

	dutyTrace, mod = tracer.getOrCreateValidatorTrace(slot3, spectypes.BNRoleProposer, validatorPK2)
	mod.Validator = phase0.ValidatorIndex(2)
	require.NotNil(t, dutyTrace)

	slot4 := phase0.Slot(4)

	dutyTrace, mod = tracer.getOrCreateValidatorTrace(slot4, spectypes.BNRoleProposer, validatorPK1)
	mod.Validator = phase0.ValidatorIndex(1)
	require.NotNil(t, dutyTrace)

	dutyTrace, mod = tracer.getOrCreateValidatorTrace(slot4, spectypes.BNRoleProposer, validatorPK2)
	mod.Validator = phase0.ValidatorIndex(2)
	require.NotNil(t, dutyTrace)

	slot5 := phase0.Slot(5)

	dutyTrace, mod = tracer.getOrCreateValidatorTrace(slot5, spectypes.BNRoleProposer, validatorPK1)
	mod.Validator = phase0.ValidatorIndex(1)
	require.NotNil(t, dutyTrace)

	dutyTrace, mod = tracer.getOrCreateValidatorTrace(slot5, spectypes.BNRoleProposer, validatorPK2)
	mod.Validator = phase0.ValidatorIndex(2)
	require.NotNil(t, dutyTrace)

	slot6 := phase0.Slot(6)

	// evict traces for slot 6 - meaning that slot 3 and 4 should be evicted (tolernace=2)
	tracer.evictValidatorTraces(slot6)

	var slot5count int
	tracer.validatorTraces.Range(func(key spectypes.ValidatorPK, slotToTraceMap *TypedSyncMap[phase0.Slot, *validatorDutyTrace]) bool {
		slotToTraceMap.Range(func(slot phase0.Slot, dutyTrace *validatorDutyTrace) bool {
			if slot == slot5 {
				slot5count++
			}
			if slot <= slot4 {
				t.Fatalf("slot %d not evicted", slot)
			}
			return true
		})
		return true
	})

	require.Equal(t, 2, slot5count)

	storedDuty3_1, err := dutyStore.GetValidatorDuty(slot3, spectypes.BNRoleProposer, 1)
	require.NoError(t, err)
	require.NotNil(t, storedDuty3_1)
	assert.Equal(t, slot3, storedDuty3_1.Slot)
	assert.Equal(t, phase0.ValidatorIndex(1), storedDuty3_1.Validator)

	storedDuty3_2, err := dutyStore.GetValidatorDuty(slot3, spectypes.BNRoleProposer, 2)
	require.NoError(t, err)
	require.NotNil(t, storedDuty3_2)
	assert.Equal(t, slot3, storedDuty3_2.Slot)
	assert.Equal(t, phase0.ValidatorIndex(2), storedDuty3_2.Validator)

	storedDuty4_1, err := dutyStore.GetValidatorDuty(slot4, spectypes.BNRoleProposer, 1)
	require.NoError(t, err)
	require.NotNil(t, storedDuty4_1)
	assert.Equal(t, slot4, storedDuty4_1.Slot)
	assert.Equal(t, phase0.ValidatorIndex(1), storedDuty4_1.Validator)

	storedDuty4_2, err := dutyStore.GetValidatorDuty(slot4, spectypes.BNRoleProposer, 2)
	require.NoError(t, err)
	require.NotNil(t, storedDuty4_2)
	assert.Equal(t, slot4, storedDuty4_2.Slot)
	assert.Equal(t, phase0.ValidatorIndex(2), storedDuty4_2.Validator)

	storedDuty5_1, err := dutyStore.GetValidatorDuty(slot5, spectypes.BNRoleProposer, 1)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Nil(t, storedDuty5_1)

	storedDuty5_2, err := dutyStore.GetValidatorDuty(slot5, spectypes.BNRoleProposer, 2)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Nil(t, storedDuty5_2)
}

func TestEvictValidatorCommitteeMapping(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)
	_, vstore, _ := registrystorage.NewSharesStorage(db, nil)

	tracer := NewTracer(context.TODO(), zap.NewNop(), vstore, nil, dutyStore, "BN")

	var committeeID1 spectypes.CommitteeID
	committeeID1[0] = 1

	var committeeID2 spectypes.CommitteeID
	committeeID2[0] = 2

	/*
		test plan:

		1. insert slots [3, 4, 5]
			index 1 ->
				slot3: committee 1
				slot4: committee 2
				slot5: committee 1
			index 2 ->
				slot3: committee 2,
				slot4: committee 1,
				slot5: committee 2,

		2. evict slot 6 which will evict slots [3, 4] (6 - tolerance(2) = 4)
		3. assert
				slots [3, 4] are deleted from cache
				slots [3, 4] are inserted into database
				slots [5] is in cache
				slots [5] is not inserted into database
	*/
	slot3 := phase0.Slot(3)
	tracer.saveValidatorToCommitteeLink(slot3, &spectypes.PartialSignatureMessages{
		Messages: []*spectypes.PartialSignatureMessage{{ValidatorIndex: 1}},
	}, committeeID1)
	tracer.saveValidatorToCommitteeLink(slot3, &spectypes.PartialSignatureMessages{
		Messages: []*spectypes.PartialSignatureMessage{{ValidatorIndex: 2}},
	}, committeeID2)

	slot4 := phase0.Slot(4)
	tracer.saveValidatorToCommitteeLink(slot4, &spectypes.PartialSignatureMessages{
		Messages: []*spectypes.PartialSignatureMessage{{ValidatorIndex: 2}},
	}, committeeID1)
	tracer.saveValidatorToCommitteeLink(slot4, &spectypes.PartialSignatureMessages{
		Messages: []*spectypes.PartialSignatureMessage{{ValidatorIndex: 1}},
	}, committeeID2)

	slot5 := phase0.Slot(5)
	tracer.saveValidatorToCommitteeLink(slot5, &spectypes.PartialSignatureMessages{
		Messages: []*spectypes.PartialSignatureMessage{{ValidatorIndex: 1}},
	}, committeeID1)
	tracer.saveValidatorToCommitteeLink(slot5, &spectypes.PartialSignatureMessages{
		Messages: []*spectypes.PartialSignatureMessage{{ValidatorIndex: 2}},
	}, committeeID2)

	// evict validator committee mapping for slot 6 - meaning that slot 3 and 4 should be evicted
	thresholdSlot := phase0.Slot(4 + ttlMapping)
	tracer.evictValidatorCommitteeLinks(thresholdSlot)

	// check that slot 3 and 4 are evicted from cache
	indexToSlotMap, found := tracer.validatorIndexToCommitteeLinks.Load(1)
	require.True(t, found)

	assert.False(t, indexToSlotMap.Has(slot3))
	assert.False(t, indexToSlotMap.Has(slot4))
	assert.True(t, indexToSlotMap.Has(slot5))

	// check that slot 3 and 4 are still in the database
	link3_1, err := dutyStore.GetCommitteeDutyLink(slot3, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID1, link3_1)

	link3_2, err := dutyStore.GetCommitteeDutyLink(slot3, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID2, link3_2)

	link4_1, err := dutyStore.GetCommitteeDutyLink(slot4, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID2, link4_1)

	link4_2, err := dutyStore.GetCommitteeDutyLink(slot4, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID1, link4_2)

	// check that slot 5 is not in the database
	_, err = dutyStore.GetCommitteeDutyLink(slot5, 1)
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = dutyStore.GetCommitteeDutyLink(slot5, 2)
	require.ErrorIs(t, err, store.ErrNotFound)
}
