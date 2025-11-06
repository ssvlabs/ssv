package validator

import (
	"encoding/binary"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	registrymocks "github.com/ssvlabs/ssv/registry/storage/mocks"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

// setCommitteeLink is a test helper that directly sets validator-to-committee links in the collector's in-memory map
func setCommitteeLink(c *Collector, slot phase0.Slot, validatorIndex phase0.ValidatorIndex, committeeID spectypes.CommitteeID) {
	slotToCommittee, found := c.validatorIndexToCommitteeLinks.Get(validatorIndex)
	if !found {
		slotToCommittee, _ = c.validatorIndexToCommitteeLinks.GetOrSet(validatorIndex, hashmap.New[phase0.Slot, spectypes.CommitteeID]())
	}
	slotToCommittee.Set(slot, committeeID)
}

func TestValidatorCommitteeMapping(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)
	_, vstore, _ := registrystorage.NewSharesStorage(networkconfig.TestNetwork.Beacon, db, dummyGetFeeRecipient, nil)

	collector := New(zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.Beacon, nil, nil)

	var committeeID1 spectypes.CommitteeID
	committeeID1[0] = 1

	var committeeID2 spectypes.CommitteeID
	committeeID2[0] = 2

	slot4 := phase0.Slot(4)
	setCommitteeLink(collector, slot4, 2, committeeID1)
	setCommitteeLink(collector, slot4, 1, committeeID2)

	slot5 := phase0.Slot(5)
	setCommitteeLink(collector, slot5, 1, committeeID1)
	setCommitteeLink(collector, slot5, 2, committeeID2)

	// assert that validator committee mapping is available (in memory)
	cmt1, err := collector.getCommitteeIDBySlotAndIndex(slot4, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt1)

	cmt2, err := collector.getCommitteeIDBySlotAndIndex(slot4, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt2)

	cmt1, err = collector.getCommitteeIDBySlotAndIndex(slot5, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt1)

	cmt2, err = collector.getCommitteeIDBySlotAndIndex(slot5, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt2)

	// evict validator committee mapping in slot 4
	thresholdSlot := phase0.Slot(4)
	collector.dumpLinkToDBPeriodically(thresholdSlot)

	// check that slot 4 is evicted from cache
	indexToSlotMap, found := collector.validatorIndexToCommitteeLinks.Get(1)
	require.True(t, found)

	assert.False(t, indexToSlotMap.Has(slot4))
	assert.True(t, indexToSlotMap.Has(slot5))

	// assert that validator committee mapping is available (on disk and in memory)
	cmt1, err = collector.getCommitteeIDBySlotAndIndex(slot4, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt1)

	cmt2, err = collector.getCommitteeIDBySlotAndIndex(slot4, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt2)

	cmt1, err = collector.getCommitteeIDBySlotAndIndex(slot5, 1)
	require.NoError(t, err)
	require.Equal(t, committeeID1, cmt1)

	cmt2, err = collector.getCommitteeIDBySlotAndIndex(slot5, 2)
	require.NoError(t, err)
	require.Equal(t, committeeID2, cmt2)

	// check that slot 4 is still in the database
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
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	vstore := registrymocks.NewMockValidatorStore(ctrl)

	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dutyStore := store.New(db)

	// setup validator index mapping
	index1 := setupValidatorStoreMock(vstore, 1)

	collector := New(zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.Beacon, nil, nil)

	var committeeID1 spectypes.CommitteeID
	committeeID1[0] = 1

	var committeeID2 spectypes.CommitteeID
	committeeID2[0] = 2

	// three slots X two committees
	slot4 := phase0.Slot(4)

	dutyTrace3, _, err := collector.getOrCreateCommitteeTrace(slot4, committeeID1)
	require.NoError(t, err)
	dutyTrace3.Decideds = append(dutyTrace3.Decideds, &exporter.DecidedTrace{
		Signers: []spectypes.OperatorID{1},
	})
	require.NotNil(t, dutyTrace3)

	dutyTrace4, _, err := collector.getOrCreateCommitteeTrace(slot4, committeeID2)
	require.NoError(t, err)
	require.NotNil(t, dutyTrace4)

	slot7 := phase0.Slot(7)

	dutyTrace5, _, err := collector.getOrCreateCommitteeTrace(slot7, committeeID1)
	require.NoError(t, err)
	dutyTrace5.Decideds = append(dutyTrace5.Decideds, &exporter.DecidedTrace{
		Signers: []spectypes.OperatorID{1},
	})
	require.NotNil(t, dutyTrace5)

	dutyTrace6, _, err := collector.getOrCreateCommitteeTrace(slot7, committeeID2)
	require.NoError(t, err)
	require.NotNil(t, dutyTrace6)

	// breakdown duties by committee
	dutiesC1 := []*committeeDutyTrace{dutyTrace3, dutyTrace5}
	dutiesC2 := []*committeeDutyTrace{dutyTrace4, dutyTrace6}

	// assert that traces are in available (in memory)
	{
		for i, slot := range []phase0.Slot{slot4, slot7} {
			dutyTrace, err := collector.GetCommitteeDuty(slot, committeeID1)
			require.NoError(t, err)
			require.NotNil(t, dutyTrace)
			assert.Equal(t, slot, dutyTrace.Slot)
			assert.Equal(t, slot, dutiesC1[i].Slot)
		}
		for i, slot := range []phase0.Slot{slot4, slot7} {
			dutyTrace, err := collector.GetCommitteeDuty(slot, committeeID2)
			require.NoError(t, err)
			require.NotNil(t, dutyTrace)
			assert.Equal(t, slot, dutyTrace.Slot)
			assert.Equal(t, slot, dutiesC2[i].Slot)
		}

		// assert that decideds are available (in memory)
		for _, slot := range []phase0.Slot{slot4, slot7} {
			setCommitteeLink(collector, slot, index1, committeeID1)
			dd, err := collector.GetCommitteeDecideds(slot, index1)
			require.NoError(t, err)
			require.NotNil(t, dd)
			require.Len(t, dd, 1)
			require.Equal(t, []spectypes.OperatorID{1}, dd[0].Signers)
		}
	}

	// step 2: evict traces at threshold 4
	// meaning that slot 4 should be evicted to disk
	// but slot 7 should be in memory
	slot8 := phase0.Slot(4)
	collector.dumpCommitteeToDBPeriodically(slot8)
	collector.dumpLinkToDBPeriodically(slot8)

	// step 3: retrieve trace from disk (4) and memory (7)
	{
		for i, slot := range []phase0.Slot{slot4, slot7} {
			dutyTrace, err := collector.GetCommitteeDuty(slot, committeeID1)
			require.NoError(t, err)
			require.NotNil(t, dutyTrace)
			assert.Equal(t, slot, dutyTrace.Slot)
			assert.Equal(t, slot, dutiesC1[i].Slot)
			assert.Equal(t, committeeID1, dutiesC1[i].CommitteeID)
		}
		for i, slot := range []phase0.Slot{slot4, slot7} {
			dutyTrace, err := collector.GetCommitteeDuty(slot, committeeID2)
			require.NoError(t, err)
			require.NotNil(t, dutyTrace)
			assert.Equal(t, slot, dutyTrace.Slot)
			assert.Equal(t, slot, dutiesC2[i].Slot)
			assert.Equal(t, committeeID2, dutiesC2[i].CommitteeID)
		}

		for _, slot := range []phase0.Slot{slot4, slot7} {
			dd, err := collector.GetCommitteeDecideds(slot, index1)
			require.NoError(t, err)
			require.NotNil(t, dd)
			require.Len(t, dd, 1)
			require.Equal(t, []spectypes.OperatorID{1}, dd[0].Signers)
		}
	}

	// assert that only slot 7 is in memory
	var inMem = make(map[phase0.Slot]struct{})
	collector.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
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
		dutyTrace5.SyncCommittee = append(dutyTrace5.SyncCommittee, &exporter.SignerData{Signer: 1})
		dutyTrace5.SyncCommittee = append(dutyTrace5.SyncCommittee, &exporter.SignerData{Signer: 2})
		dutyTrace5.Attester = append(dutyTrace5.Attester, &exporter.SignerData{Signer: 3})
		dd, err := collector.GetCommitteeDecideds(slot7, index1)
		require.NoError(t, err)
		require.NotNil(t, dd)
		require.Len(t, dd, 1)
		require.Equal(t, []spectypes.OperatorID{1, 2, 3}, dd[0].Signers)
	}

	err = dutyStore.SaveCommitteeDutyLink(slot7, index1, committeeID1)
	require.NoError(t, err)

	dd, err := collector.GetAllCommitteeDecideds(slot7)
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 2)

	signers := []spectypes.OperatorID{}
	for _, decided := range dd {
		signers = append(signers, decided.Signers...)
	}
	require.Equal(t, []spectypes.OperatorID{1, 2, 3}, signers)
}

func TestCommitteeDutyStore_GetAllCommitteeDecideds(t *testing.T) {
	validatorPK7 := spectypes.ValidatorPK{7}
	committeeID1 := spectypes.CommitteeID{1}
	slot4 := phase0.Slot(4)
	index1 := phase0.ValidatorIndex(1)

	// Setup db, shares & collector
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	require.NoError(t, err)
	dutyStore := store.New(db)
	err = db.Set([]byte("val_pki"), validatorPK7[:], encodeLittleEndian(index1))
	require.NoError(t, err)

	shares, vstore, _ := registrystorage.NewSharesStorage(networkconfig.TestNetwork.Beacon, db, dummyGetFeeRecipient, nil)
	shares.Save(db, &types.SSVShare{
		Status: eth2apiv1.ValidatorStateActiveOngoing,
		Share: spectypes.Share{
			ValidatorIndex:  index1,
			ValidatorPubKey: validatorPK7,
		},
	})
	collector := New(zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.Beacon, nil, nil)

	// Create a new trace
	dutyTrace, _, err := collector.getOrCreateCommitteeTrace(slot4, committeeID1)
	require.NoError(t, err)
	dutyTrace.Decideds = append(dutyTrace.Decideds, &exporter.DecidedTrace{
		Signers: []spectypes.OperatorID{1},
	})
	require.NotNil(t, dutyTrace)
	setCommitteeLink(collector, slot4, index1, committeeID1)

	// Fetch trace from memory cache and check
	{
		dd, err := collector.GetAllCommitteeDecideds(slot4)
		require.NoError(t, err)
		require.NotNil(t, dd)
		require.Len(t, dd, 1)
		require.Equal(t, index1, dd[0].Index)
	}

	// Evict to disk
	collector.dumpCommitteeToDBPeriodically(slot4)
	collector.dumpLinkToDBPeriodically(slot4)

	// Fetch trace from disk and check
	{
		dd, err := collector.GetAllCommitteeDecideds(slot4)
		require.NoError(t, err)
		require.NotNil(t, dd)
		require.Len(t, dd, 1)
		require.Equal(t, index1, dd[0].Index)
	}
}

func setupValidatorStoreMock(store *registrymocks.MockValidatorStore, idx int) phase0.ValidatorIndex {
	index := phase0.ValidatorIndex(idx)
	var validatorPK spectypes.ValidatorPK
	binary.BigEndian.PutUint32(validatorPK[:], uint32(idx))

	store.EXPECT().Validator(validatorPK).Return(&types.SSVShare{Share: spectypes.Share{ValidatorIndex: index, ValidatorPubKey: validatorPK}}, true).AnyTimes()
	store.EXPECT().ValidatorIndex(validatorPK).Return(index, true).AnyTimes()
	store.EXPECT().ValidatorByIndex(index).Return(&types.SSVShare{Share: spectypes.Share{ValidatorIndex: index, ValidatorPubKey: validatorPK}}, true).AnyTimes()
	return index
}

func TestValidatorDutyStore(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	vstore := registrymocks.NewMockValidatorStore(ctrl)

	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dutyStore := store.New(db)

	// setup validator pubkey -> index mapping
	// this is used to get the validator index from the pubkey
	// when the duty is not found in the cache
	// because on disk the validator index is stored
	index1 := setupValidatorStoreMock(vstore, 1)
	index2 := setupValidatorStoreMock(vstore, 2)

	collector := New(zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.Beacon, nil, nil)

	slot4 := phase0.Slot(4)

	dutyTrace, _, err := collector.getOrCreateValidatorTrace(slot4, spectypes.BNRoleProposer, index1)
	require.NoError(t, err)
	roleDutyTrace := dutyTrace.getOrCreate(slot4, spectypes.BNRoleProposer)
	roleDutyTrace.Validator = index1
	roleDutyTrace.Decideds = append(roleDutyTrace.Decideds, &exporter.DecidedTrace{
		Signers: []spectypes.OperatorID{1},
	})

	require.NotNil(t, dutyTrace)

	dutyTrace, _, err = collector.getOrCreateValidatorTrace(slot4, spectypes.BNRoleProposer, index2)
	require.NoError(t, err)
	roleDutyTrace = dutyTrace.getOrCreate(slot4, spectypes.BNRoleProposer)
	roleDutyTrace.Validator = index2
	require.NotNil(t, dutyTrace)

	slot7 := phase0.Slot(7)

	dutyTrace, _, err = collector.getOrCreateValidatorTrace(slot7, spectypes.BNRoleProposer, index1)
	require.NoError(t, err)
	roleDutyTrace = dutyTrace.getOrCreate(slot7, spectypes.BNRoleProposer)
	roleDutyTrace.Validator = index1
	roleDutyTrace.Decideds = append(roleDutyTrace.Decideds, &exporter.DecidedTrace{
		Signers: []spectypes.OperatorID{5},
	})

	require.NotNil(t, dutyTrace)

	dutyTrace, _, err = collector.getOrCreateValidatorTrace(slot7, spectypes.BNRoleProposer, index2)
	require.NoError(t, err)
	roleDutyTrace = dutyTrace.getOrCreate(slot7, spectypes.BNRoleProposer)
	roleDutyTrace.Validator = index2
	require.NotNil(t, dutyTrace)

	dd, err := collector.GetValidatorDecideds(spectypes.BNRoleProposer, slot4, []phase0.ValidatorIndex{index1})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{1}, dd[0].Signers)

	dd, err = collector.GetValidatorDecideds(spectypes.BNRoleProposer, slot7, []phase0.ValidatorIndex{index1})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{5}, dd[0].Signers)

	// test that decideds include signers in the 'Post' consensus messages
	roleDutyTrace = dutyTrace.getOrCreate(slot7, spectypes.BNRoleProposer)
	roleDutyTrace.Post = append(roleDutyTrace.Post, &exporter.PartialSigTrace{Signer: 99})
	roleDutyTrace.Post = append(roleDutyTrace.Post, &exporter.PartialSigTrace{Signer: 100})
	roleDutyTrace.Decideds = append(roleDutyTrace.Decideds, &exporter.DecidedTrace{
		Signers: []spectypes.OperatorID{100},
	})
	dd, err = collector.GetValidatorDecideds(spectypes.BNRoleProposer, slot7, []phase0.ValidatorIndex{index2})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{99, 100}, dd[0].Signers)

	// evict slot 4
	threshold := phase0.Slot(4)
	collector.dumpValidatorToDBPeriodically(threshold)

	var inMem = make(map[phase0.Slot]struct{})
	collector.validatorTraces.Range(func(key phase0.ValidatorIndex, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
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
	dd, err = collector.GetValidatorDecideds(spectypes.BNRoleProposer, slot4, []phase0.ValidatorIndex{index1})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{1}, dd[0].Signers)

	dd, err = collector.GetValidatorDecideds(spectypes.BNRoleProposer, slot7, []phase0.ValidatorIndex{index1})
	require.NoError(t, err)
	require.NotNil(t, dd)
	require.Len(t, dd, 1)
	require.Equal(t, []spectypes.OperatorID{5}, dd[0].Signers)

	// assert that evicted traces are on disk
	storedDuty4_1, err := dutyStore.GetValidatorDuty(slot4, spectypes.BNRoleProposer, 1)
	require.NoError(t, err)
	require.NotNil(t, storedDuty4_1)
	assert.Equal(t, slot4, storedDuty4_1.Slot)
	assert.Equal(t, phase0.ValidatorIndex(1), storedDuty4_1.Validator)

	storedDuty4_2, err := dutyStore.GetValidatorDuty(slot4, spectypes.BNRoleProposer, 2)
	require.NoError(t, err)
	require.NotNil(t, storedDuty4_2)
	assert.Equal(t, slot4, storedDuty4_2.Slot)
	assert.Equal(t, index2, storedDuty4_2.Validator)

	// assert non-evicted traces are not on disk
	storedDuty7_1, err := dutyStore.GetValidatorDuty(slot7, spectypes.BNRoleProposer, 1)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Nil(t, storedDuty7_1)

	storedDuty7_2, err := dutyStore.GetValidatorDuty(slot7, spectypes.BNRoleProposer, 2)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Nil(t, storedDuty7_2)

	_, err = collector.GetAllValidatorDecideds(spectypes.BNRoleProposer, slot4)
	require.NoError(t, err)
}

// --- helpers -------------------------------------------

func encodeLittleEndian(i phase0.ValidatorIndex) []byte {
	value := make([]byte, 8)
	binary.LittleEndian.PutUint64(value, uint64(i))
	return value
}
