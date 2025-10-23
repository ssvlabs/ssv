//go:build lfs

package validator

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/pebble"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func TestEviction(t *testing.T) {
	f, err := os.OpenFile("./benchdata/slot_3707881_3707882.ssz", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}

	traces, err := readByteSlices(f) // len(traces) = 8992
	if err != nil {
		t.Fatal(err)
	}

	_ = f.Close()

	db, err := kv.New(zap.NewNop(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)
	_, vstore, _ := storage.NewSharesStorage(networkconfig.TestNetwork.Beacon, db, dummyGetFeeRecipient, nil)

	collector := New(zap.NewNop(), vstore, mockDomainDataProvider{}, dutyStore, networkconfig.TestNetwork.Beacon, nil, nil)

	for _, trace := range traces {
		collector.Collect(t.Context(), trace, dummyVerify)
	}

	slot1, slot2 := phase0.Slot(3707881+slotTTL), phase0.Slot(3707882+slotTTL)

	collector.evict(slot1)
	collector.evict(slot2)

	collector.validatorTraces.Range(func(_ phase0.ValidatorIndex, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		_, found := slotToTraceMap.Get(slot1)
		if found {
			t.Fatalf("validator: slot %d not evicted", slot1)
		}
		_, found = slotToTraceMap.Get(slot2)
		if found {
			t.Fatalf("validator: slot %d not evicted", slot2)
		}
		return true
	})

	collector.committeeTraces.Range(func(committeeID spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		_, found := slotToTraceMap.Get(slot1)
		if found {
			t.Fatalf("committee: slot %d not evicted", slot1)
		}
		_, found = slotToTraceMap.Get(slot2)
		if found {
			t.Fatalf("committee: slot %d not evicted", slot2)
		}
		return true
	})

	collector.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		_, found := slotToCommittee.Get(slot1)
		if found {
			t.Fatalf("committee: slot %d not evicted", slot1)
		}
		_, found = slotToCommittee.Get(slot2)
		if found {
			t.Fatalf("committee: slot %d not evicted", slot2)
		}
		return true
	})

	{
		// validator
		duties, err := dutyStore.GetValidatorDuties(spectypes.BNRoleAggregator, 3707881)
		require.NoError(t, err)
		require.Len(t, duties, 652)

		duties, err = dutyStore.GetValidatorDuties(spectypes.BNRoleValidatorRegistration, 3707881)
		require.NoError(t, err)
		require.Len(t, duties, 63)

		duties, err = dutyStore.GetValidatorDuties(spectypes.BNRoleSyncCommitteeContribution, 3707881)
		require.NoError(t, err)
		require.Len(t, duties, 4)

		duties, err = dutyStore.GetValidatorDuties(spectypes.BNRoleAggregator, 3707882)
		require.NoError(t, err)
		require.Len(t, duties, 657)

		duties, err = dutyStore.GetValidatorDuties(spectypes.BNRoleValidatorRegistration, 3707881)
		require.NoError(t, err)
		require.Len(t, duties, 63)

		duties, err = dutyStore.GetValidatorDuties(spectypes.BNRoleSyncCommitteeContribution, 3707881)
		require.NoError(t, err)
		require.Len(t, duties, 4)
	}

	{
		// committee
		duties, err := dutyStore.GetCommitteeDuties(3707881)
		require.NoError(t, err)
		require.Len(t, duties, 102)

		duties, err = dutyStore.GetCommitteeDuties(3707882)
		require.NoError(t, err)
		require.Len(t, duties, 96)
	}

	{
		// links
		links, err := dutyStore.GetCommitteeDutyLinks(3707881)
		require.NoError(t, err)
		require.Len(t, links, 787)

		links, err = dutyStore.GetCommitteeDutyLinks(3707882)
		require.NoError(t, err)
		require.Len(t, links, 799)
	}
}

func TestPendingDetails_FieldShape(t *testing.T) {
	// Build a small pending map with one root, one signer and two timestamp buckets
	var root phase0.Root
	copy(root[:], []byte{1, 2, 3})
	signer := spectypes.OperatorID(99)
	ts1 := uint64(1000)
	ts2 := uint64(2000)

	data := map[phase0.Root]map[spectypes.OperatorID]map[uint64][]phase0.ValidatorIndex{
		root: {
			signer: {
				ts1: {phase0.ValidatorIndex(5), phase0.ValidatorIndex(5), phase0.ValidatorIndex(7)},
				ts2: {phase0.ValidatorIndex(7), phase0.ValidatorIndex(9)},
			},
		},
	}

	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	logger.Debug("pending-details", pendingDetails(data))

	entries := logs.All()
	require.Len(t, entries, 1)
	entry := entries[0]

	// Find our field
	var fieldFound bool
	var payload any
	for _, f := range entry.Context {
		if f.Key == "pending_signers_by_root" {
			fieldFound = true
			payload = f.Interface
			break
		}
	}
	require.True(t, fieldFound, "expected pending_signers_by_root field")

	// Decode the shape: map[rootHex] -> map[signer] -> { union_indices, by_timestamp }
	m, ok := payload.(map[string]map[string]any)
	if !ok {
		// fallback: cope with reflection producing slightly different types
		// but still validate that the field is a map
		require.FailNow(t, "unexpected payload type for pending_signers_by_root: %T", payload)
	}

	rhex := hex.EncodeToString(root[:])
	inner, ok := m[rhex]
	require.True(t, ok)

	skey := "99"
	sdata, ok := inner[skey].(map[string]any)
	require.True(t, ok)

	// union_indices should contain 5,7,9 (dedup + sort)
	union, ok := sdata["union_indices"].([]uint64)
	require.True(t, ok)
	assert.ElementsMatch(t, []uint64{5, 7, 9}, union)

	// by_timestamp should be a list ordered by t ascending
	buckets, ok := sdata["by_timestamp"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, buckets, 2)

	// First bucket is ts1
	t1, ok := buckets[0]["t"].(uint64)
	require.True(t, ok)
	t2, ok := buckets[1]["t"].(uint64)
	require.True(t, ok)
	assert.Less(t, t1, t2)

	// Indices content
	idxs1, ok := buckets[0]["indices"].([]uint64)
	require.True(t, ok)
	assert.ElementsMatch(t, []uint64{5, 7}, idxs1)
	idxs2, ok := buckets[1]["indices"].([]uint64)
	require.True(t, ok)
	assert.ElementsMatch(t, []uint64{7, 9}, idxs2)
}
