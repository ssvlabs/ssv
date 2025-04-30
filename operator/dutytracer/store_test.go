package validator

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/exporter/v2/store"
	"github.com/ssvlabs/ssv/networkconfig"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func TestValidatorCommitteeMapping(t *testing.T) {
	db, err := badger.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)
	_, vstore, _ := registrystorage.NewSharesStorage(networkconfig.LocalTestnet, db, nil)

	tracer := New(context.TODO(), zap.NewNop(), vstore, nil, dutyStore, "BN")

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

		2. evict slots 4 and lower
		3. assert
				slots [3, 4] are deleted from cache
				slots [3, 4] are inserted into database
				slots [5] is in cache but not in the db
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

	// assert that validator committee mapping is available (in memory)
	cmt1, err := tracer.getCommitteeIDBySlotAndIndex(slot3, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt1)

	cmt2, err := tracer.getCommitteeIDBySlotAndIndex(slot3, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt2)

	cmt1, err = tracer.getCommitteeIDBySlotAndIndex(slot4, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt1)

	cmt2, err = tracer.getCommitteeIDBySlotAndIndex(slot4, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt2)

	cmt1, err = tracer.getCommitteeIDBySlotAndIndex(slot5, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt1)

	cmt2, err = tracer.getCommitteeIDBySlotAndIndex(slot5, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt2)

	// evict validator committee mapping in slot 4 and lower
	thresholdSlot := phase0.Slot(4)
	tracer.evictValidatorCommitteeLinks(thresholdSlot)

	// check that slot 3 and 4 are evicted from cache
	indexToSlotMap, found := tracer.validatorIndexToCommitteeLinks.Get(1)
	require.True(t, found)

	assert.False(t, indexToSlotMap.Has(slot3))
	assert.False(t, indexToSlotMap.Has(slot4))
	assert.True(t, indexToSlotMap.Has(slot5))

	// assert that validator committee mapping is available (on disk and in memory)
	cmt1, err = tracer.getCommitteeIDBySlotAndIndex(slot3, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt1)

	cmt2, err = tracer.getCommitteeIDBySlotAndIndex(slot3, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt2)

	cmt1, err = tracer.getCommitteeIDBySlotAndIndex(slot4, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt1)

	cmt2, err = tracer.getCommitteeIDBySlotAndIndex(slot4, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt2)

	cmt1, err = tracer.getCommitteeIDBySlotAndIndex(slot5, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt1)

	cmt2, err = tracer.getCommitteeIDBySlotAndIndex(slot5, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt2)

	// check that slots 3 and 4 are still in the database
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

func TestCommitteeDutyStore(t *testing.T) {
	db, err := badger.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)

	// setup validator validatorPK -> index mapping
	var validatorPK spectypes.ValidatorPK
	validatorPK[0] = 7

	var index = phase0.ValidatorIndex(1)
	value := make([]byte, 8)
	binary.LittleEndian.PutUint64(value, uint64(index))

	err = db.Set([]byte("val_pki"), validatorPK[:], value)
	require.NoError(t, err)

	_, vstore, _ := registrystorage.NewSharesStorage(networkconfig.LocalTestnet, db, nil)

	tracer := New(context.TODO(), zap.NewNop(), vstore, nil, dutyStore, "BN")

	var committeeID1 spectypes.CommitteeID
	committeeID1[0] = 1

	var committeeID2 spectypes.CommitteeID
	committeeID2[0] = 2

	// three slots X two committees
	slot3 := phase0.Slot(3)

	dutyTrace1, _, err := tracer.getOrCreateCommitteeTrace(slot3, committeeID1)
	require.NoError(t, err)
	dutyTrace1.Decideds = append(dutyTrace1.Decideds, &model.DecidedTrace{
		Signers: []spectypes.OperatorID{1},
	})
	require.NotNil(t, dutyTrace1)

	dutyTrace2, _, err := tracer.getOrCreateCommitteeTrace(slot3, committeeID2)
	require.NoError(t, err)
	require.NotNil(t, dutyTrace2)

	slot4 := phase0.Slot(4)

	dutyTrace3, _, err := tracer.getOrCreateCommitteeTrace(slot4, committeeID1)
	require.NoError(t, err)
	dutyTrace3.Decideds = append(dutyTrace3.Decideds, &model.DecidedTrace{
		Signers: []spectypes.OperatorID{1},
	})
	require.NotNil(t, dutyTrace3)

	dutyTrace4, _, err := tracer.getOrCreateCommitteeTrace(slot4, committeeID2)
	require.NoError(t, err)
	require.NotNil(t, dutyTrace4)

	slot7 := phase0.Slot(7)

	dutyTrace5, _, err := tracer.getOrCreateCommitteeTrace(slot7, committeeID1)
	require.NoError(t, err)
	dutyTrace5.Decideds = append(dutyTrace5.Decideds, &model.DecidedTrace{
		Signers: []spectypes.OperatorID{1},
	})
	require.NotNil(t, dutyTrace5)

	dutyTrace6, _, err := tracer.getOrCreateCommitteeTrace(slot7, committeeID2)
	require.NoError(t, err)
	require.NotNil(t, dutyTrace6)

	// breakdown duties by committee
	dutiesC1 := []*committeeDutyTrace{dutyTrace1, dutyTrace3, dutyTrace5}
	dutiesC2 := []*committeeDutyTrace{dutyTrace2, dutyTrace4, dutyTrace6}

	// assert that traces are in available (in memory)
	{
		for i, slot := range []phase0.Slot{slot3, slot4, slot7} {
			dutyTrace, err := tracer.GetCommitteeDuty(slot, committeeID1)
			require.NoError(t, err)
			require.NotNil(t, dutyTrace)
			assert.Equal(t, slot, dutyTrace.Slot)
			assert.Equal(t, slot, dutiesC1[i].Slot)
		}
		for i, slot := range []phase0.Slot{slot3, slot4, slot7} {
			dutyTrace, err := tracer.GetCommitteeDuty(slot, committeeID2)
			require.NoError(t, err)
			require.NotNil(t, dutyTrace)
			assert.Equal(t, slot, dutyTrace.Slot)
			assert.Equal(t, slot, dutiesC2[i].Slot)
		}

		// assert that decideds are available (in memory)
		for _, slot := range []phase0.Slot{slot3, slot4, slot7} {
			tracer.saveValidatorToCommitteeLink(slot, &spectypes.PartialSignatureMessages{
				Messages: []*spectypes.PartialSignatureMessage{{ValidatorIndex: index}},
			}, committeeID1)
			dd, err := tracer.GetCommitteeDecideds(slot, validatorPK)
			require.NoError(t, err)
			require.NotNil(t, dd)
			require.Len(t, dd, 1)
			require.Equal(t, []spectypes.OperatorID{1}, dd[0].Signers)
		}
	}

	// step 2: evict traces at threshold 4
	// meaning that slot 3 and 4 should be evicted to disk
	// but slot 7 should be in memory
	slot8 := phase0.Slot(4)
	tracer.evictCommitteeTraces(slot8)
	tracer.evictValidatorCommitteeLinks(slot8)

	// step 3: retrieve trace from disk (3,4) and memory (7)
	{
		for i, slot := range []phase0.Slot{slot3, slot4, slot7} {
			dutyTrace, err := tracer.GetCommitteeDuty(slot, committeeID1)
			require.NoError(t, err)
			require.NotNil(t, dutyTrace)
			assert.Equal(t, slot, dutyTrace.Slot)
			assert.Equal(t, slot, dutiesC1[i].Slot)
			assert.Equal(t, committeeID1, dutiesC1[i].CommitteeID)
		}
		for i, slot := range []phase0.Slot{slot3, slot4, slot7} {
			dutyTrace, err := tracer.GetCommitteeDuty(slot, committeeID2)
			require.NoError(t, err)
			require.NotNil(t, dutyTrace)
			assert.Equal(t, slot, dutyTrace.Slot)
			assert.Equal(t, slot, dutiesC2[i].Slot)
			assert.Equal(t, committeeID2, dutiesC2[i].CommitteeID)
		}

		for _, slot := range []phase0.Slot{slot3, slot4, slot7} {
			dd, err := tracer.GetCommitteeDecideds(slot, validatorPK)
			require.NoError(t, err)
			require.NotNil(t, dd)
			require.Len(t, dd, 1)
			require.Equal(t, []spectypes.OperatorID{1}, dd[0].Signers)
		}
	}

	// assert that only slot 7 is in memory
	var inMem = make(map[phase0.Slot]struct{})
	tracer.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		slotToTraceMap.Range(func(slot phase0.Slot, dutyTrace *committeeDutyTrace) bool {
			inMem[slot] = struct{}{}
			return true
		})
		return true
	})

	require.Len(t, inMem, 1)
	_, found := inMem[slot7]
	require.True(t, found)

	// assert that evicted traces are on disk
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

	// assert that non-evicted traces are not on disk
	storedDuty7_1, err := dutyStore.GetCommitteeDuty(slot7, committeeID1)
	assert.ErrorIs(t, err, store.ErrNotFound)
	require.Nil(t, storedDuty7_1)

	storedDuty7_2, err := dutyStore.GetCommitteeDuty(slot7, committeeID2)
	assert.ErrorIs(t, err, store.ErrNotFound)
	require.Nil(t, storedDuty7_2)

	{ // check that sync committee and attester signers are included in decideds
		dutyTrace5.SyncCommittee = append(dutyTrace5.SyncCommittee, &model.SignerData{Signer: 1})
		dutyTrace5.SyncCommittee = append(dutyTrace5.SyncCommittee, &model.SignerData{Signer: 2})
		dutyTrace5.Attester = append(dutyTrace5.Attester, &model.SignerData{Signer: 3})
		dd, err := tracer.GetCommitteeDecideds(slot7, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, dd)
		require.Len(t, dd, 1)
		require.Equal(t, []spectypes.OperatorID{1, 2, 3}, dd[0].Signers)
	}
}

func TestValidatorDutyStore(t *testing.T) {
	db, err := badger.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)

	// setup validator pubkey -> index mapping
	// this is used to get the validator index from the pubkey
	// when the duty is not found in the cache
	// because on disk the validator index is stored
	var validatorPK1 spectypes.ValidatorPK
	validatorPK1[0] = 1

	var validatorPK2 spectypes.ValidatorPK
	validatorPK2[0] = 2

	var index = phase0.ValidatorIndex(1)
	value := make([]byte, 8)
	binary.LittleEndian.PutUint64(value, uint64(index))

	err = db.Set([]byte("val_pki"), validatorPK1[:], value)
	require.NoError(t, err)

	_, vstore, _ := registrystorage.NewSharesStorage(networkconfig.LocalTestnet, db, nil)

	tracer := New(context.TODO(), zap.NewNop(), vstore, nil, dutyStore, "BN")

	slot3 := phase0.Slot(3)

	dutyTrace, mod, err := tracer.getOrCreateValidatorTrace(slot3, spectypes.BNRoleProposer, validatorPK1)
	require.NoError(t, err)
	mod.Validator = index
	require.NotNil(t, dutyTrace)

	dutyTrace, mod, err = tracer.getOrCreateValidatorTrace(slot3, spectypes.BNRoleProposer, validatorPK2)
	require.NoError(t, err)
	mod.Validator = phase0.ValidatorIndex(2)
	require.NotNil(t, dutyTrace)

	slot4 := phase0.Slot(4)

	dutyTrace, mod, err = tracer.getOrCreateValidatorTrace(slot4, spectypes.BNRoleProposer, validatorPK1)
	require.NoError(t, err)
	mod.Validator = index
	mod.Decideds = append(mod.Decideds, &model.DecidedTrace{
		Signers: []spectypes.OperatorID{1},
	})
	require.NotNil(t, dutyTrace)

	dutyTrace, mod, err = tracer.getOrCreateValidatorTrace(slot4, spectypes.BNRoleProposer, validatorPK2)
	require.NoError(t, err)
	mod.Validator = phase0.ValidatorIndex(2)
	require.NotNil(t, dutyTrace)

	slot7 := phase0.Slot(7)

	dutyTrace, mod, err = tracer.getOrCreateValidatorTrace(slot7, spectypes.BNRoleProposer, validatorPK1)
	require.NoError(t, err)
	mod.Validator = index
	mod.Decideds = append(mod.Decideds, &model.DecidedTrace{
		Signers: []spectypes.OperatorID{5},
	})
	require.NotNil(t, dutyTrace)

	dutyTrace, mod, err = tracer.getOrCreateValidatorTrace(slot7, spectypes.BNRoleProposer, validatorPK2)
	require.NoError(t, err)
	mod.Validator = phase0.ValidatorIndex(2)
	require.NotNil(t, dutyTrace)

	dd, err := tracer.GetValidatorDecideds(spectypes.BNRoleProposer, slot4, []spectypes.ValidatorPK{validatorPK1})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{1}, dd[0].Signers)

	dd, err = tracer.GetValidatorDecideds(spectypes.BNRoleProposer, slot7, []spectypes.ValidatorPK{validatorPK1})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{5}, dd[0].Signers)

	// test that decideds include signers in the 'Post' consensus messages
	mod.Post = append(mod.Post, &model.PartialSigTrace{Signer: 99})
	mod.Post = append(mod.Post, &model.PartialSigTrace{Signer: 100})
	mod.Decideds = append(mod.Decideds, &model.DecidedTrace{
		Signers: []spectypes.OperatorID{100},
	})
	dd, err = tracer.GetValidatorDecideds(spectypes.BNRoleProposer, slot7, []spectypes.ValidatorPK{validatorPK2})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{99, 100}, dd[0].Signers)

	// evict slot 3 and 4
	slot6 := phase0.Slot(4)
	tracer.evictValidatorTraces(slot6)

	var inMem = make(map[phase0.Slot]struct{})
	tracer.validatorTraces.Range(func(key spectypes.ValidatorPK, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		slotToTraceMap.Range(func(slot phase0.Slot, dutyTrace *validatorDutyTrace) bool {
			inMem[slot] = struct{}{}
			return true
		})
		return true
	})

	require.Len(t, inMem, 1)
	_, found := inMem[slot7]
	require.True(t, found)

	// assert that decideds are available after eviction
	dd, err = tracer.GetValidatorDecideds(spectypes.BNRoleProposer, slot4, []spectypes.ValidatorPK{validatorPK1})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{1}, dd[0].Signers)

	dd, err = tracer.GetValidatorDecideds(spectypes.BNRoleProposer, slot7, []spectypes.ValidatorPK{validatorPK1})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{5}, dd[0].Signers)

	// assert that evicted traces are on disk
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

	// assert non-evicted traces are not on disk
	storedDuty7_1, err := dutyStore.GetValidatorDuty(slot7, spectypes.BNRoleProposer, 1)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Nil(t, storedDuty7_1)

	storedDuty7_2, err := dutyStore.GetValidatorDuty(slot7, spectypes.BNRoleProposer, 2)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Nil(t, storedDuty7_2)
}
