package dutytracer

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/exporter/traces"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// TestDutyTraceStoreMetrics_CommitteeDutyDelegation verifies the metrics wrapper transparently
// delegates committee-duty reads/writes (including the role param) to the underlying store.
func TestDutyTraceStoreMetrics_CommitteeDutyDelegation(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	metricsStore := &DutyTraceStoreMetrics{Store: store.New(db)}

	slot := phase0.Slot(11)
	committeeID := spectypes.CommitteeID{9, 9, 9}
	duty := &traces.CommitteeDutyTrace{
		Slot:        slot,
		Role:        spectypes.RoleAggregatorCommittee,
		CommitteeID: committeeID,
	}

	require.NoError(t, metricsStore.SaveCommitteeDuty(spectypes.RoleAggregatorCommittee, duty))

	got, err := metricsStore.GetCommitteeDuty(slot, spectypes.RoleAggregatorCommittee, committeeID)
	require.NoError(t, err)
	assert.Equal(t, spectypes.RoleAggregatorCommittee, got.Role)
	assert.Equal(t, committeeID, got.CommitteeID)

	// Not visible under the other role: role-disjoint keyspaces.
	_, err = metricsStore.GetCommitteeDuty(slot, spectypes.RoleCommittee, committeeID)
	require.Error(t, err)

	all, err := metricsStore.GetCommitteeDuties(slot, spectypes.RoleAggregatorCommittee)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, committeeID, all[0].CommitteeID)
}

// TestDutyTraceStoreMetrics_SaveCommitteeDutiesDelegation verifies the batch-save path.
func TestDutyTraceStoreMetrics_SaveCommitteeDutiesDelegation(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	metricsStore := &DutyTraceStoreMetrics{Store: store.New(db)}

	slot := phase0.Slot(12)
	duties := []*traces.CommitteeDutyTrace{
		{Slot: slot, Role: spectypes.RoleCommittee, CommitteeID: spectypes.CommitteeID{1}},
		{Slot: slot, Role: spectypes.RoleCommittee, CommitteeID: spectypes.CommitteeID{2}},
	}

	require.NoError(t, metricsStore.SaveCommitteeDuties(slot, spectypes.RoleCommittee, duties))

	got, err := metricsStore.GetCommitteeDuties(slot, spectypes.RoleCommittee)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}
