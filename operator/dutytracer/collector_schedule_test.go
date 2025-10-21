package validator

import (
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

func TestCollectorComputeAndPersistScheduleForSlot(t *testing.T) {
	t.Parallel()

	cfg := *networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(96)
	epoch := cfg.EstimatedEpochAtSlot(slot)
	period := cfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)

	duties := dutystore.New()
	duties.Attester.Set(epoch, []dutystore.StoreDuty[eth2apiv1.AttesterDuty]{
		{
			Slot:           slot,
			ValidatorIndex: 3,
			Duty: &eth2apiv1.AttesterDuty{
				Slot:           slot,
				ValidatorIndex: 3,
			},
			InCommittee: true,
		},
	})
	duties.Proposer.Set(epoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
		{
			Slot:           slot,
			ValidatorIndex: 5,
			Duty: &eth2apiv1.ProposerDuty{
				Slot:           slot,
				ValidatorIndex: 5,
			},
		},
	})
	duties.SyncCommittee.Set(period, []dutystore.StoreSyncCommitteeDuty{
		{
			ValidatorIndex: 7,
			Duty: &eth2apiv1.SyncCommitteeDuty{
				ValidatorIndex: 7,
			},
			InCommittee: true,
		},
	})

	store := &mockDutyTraceStore{}
	collector := New(zap.NewNop(), nil, nil, store, &cfg, nil, duties)

	err := collector.computeAndPersistScheduleForSlot(slot)
	require.NoError(t, err)

	saved := store.scheduled[slot]
	require.NotNil(t, saved)

	assert.Equal(t, rolemask.BitAttester, saved[3]&rolemask.BitAttester)
	assert.Equal(t, rolemask.BitProposer, saved[5]&rolemask.BitProposer)
	assert.Equal(t, rolemask.BitSyncCommittee, saved[7]&rolemask.BitSyncCommittee)
}

func TestCollectorComputeAndPersistScheduleEmpty(t *testing.T) {
	t.Parallel()

	cfg := *networkconfig.TestNetwork.Beacon
	store := &mockDutyTraceStore{}
	collector := New(zap.NewNop(), nil, nil, store, &cfg, nil, dutystore.New())

	err := collector.computeAndPersistScheduleForSlot(phase0.Slot(10))
	require.NoError(t, err)
	assert.Nil(t, store.scheduled)
}

func TestCollectorComputeAndPersistScheduleError(t *testing.T) {
	t.Parallel()

	cfg := *networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(12)
	epoch := cfg.EstimatedEpochAtSlot(slot)

	duties := dutystore.New()
	duties.Attester.Set(epoch, []dutystore.StoreDuty[eth2apiv1.AttesterDuty]{
		{
			Slot:           slot,
			ValidatorIndex: 0,
			Duty:           &eth2apiv1.AttesterDuty{Slot: slot, ValidatorIndex: 0},
			InCommittee:    true,
		},
	})

	store := &mockDutyTraceStore{err: assert.AnError}
	collector := New(zap.NewNop(), nil, nil, store, &cfg, nil, duties)

	err := collector.computeAndPersistScheduleForSlot(slot)
	assert.ErrorIs(t, err, assert.AnError)
}
