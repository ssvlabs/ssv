//go:build lfs
// +build lfs

package validator

import (
	"os"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

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

	collector := New(zap.NewNop(), vstore, mockDomainDataProvider{}, dutyStore, networkconfig.TestNetwork.Beacon, nil)

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
