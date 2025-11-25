package validator

import (
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

type mockValidatorStore struct {
	participatingCommittees []*registrystorage.Committee
}

func (m *mockValidatorStore) Validator(pubKey []byte) (*ssvtypes.SSVShare, bool) {
	return nil, false
}

func (m *mockValidatorStore) ValidatorIndex(pubKey spectypes.ValidatorPK) (phase0.ValidatorIndex, bool) {
	return 0, false
}

func (m *mockValidatorStore) ValidatorPubkey(index phase0.ValidatorIndex) (spectypes.ValidatorPK, bool) {
	return spectypes.ValidatorPK{}, false
}

func (m *mockValidatorStore) ValidatorByIndex(index phase0.ValidatorIndex) (*ssvtypes.SSVShare, bool) {
	return nil, false
}

func (m *mockValidatorStore) Validators() []*ssvtypes.SSVShare {
	return nil
}

func (m *mockValidatorStore) Committee(id spectypes.CommitteeID) (*registrystorage.Committee, bool) {
	return nil, false
}

func (m *mockValidatorStore) ParticipatingCommittees(epoch phase0.Epoch) []*registrystorage.Committee {
	return m.participatingCommittees
}

func (m *mockValidatorStore) Committees() []*registrystorage.Committee {
	return nil
}

func (m *mockValidatorStore) ParticipatingValidators(epoch phase0.Epoch) []*ssvtypes.SSVShare {
	return nil
}

func (m *mockValidatorStore) OperatorValidators(id spectypes.OperatorID) []*ssvtypes.SSVShare {
	return nil
}

func (m *mockValidatorStore) OperatorCommittees(id spectypes.OperatorID) []*registrystorage.Committee {
	return nil
}

func (m *mockValidatorStore) GetFeeRecipient(validatorPK spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error) {
	return bellatrix.ExecutionAddress{}, nil
}

func (m *mockValidatorStore) WithOperatorID(operatorID func() spectypes.OperatorID) registrystorage.SelfValidatorStore {
	return nil
}

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

	vstore := &mockValidatorStore{
		participatingCommittees: []*registrystorage.Committee{},
	}

	collector := New(zap.NewNop(), vstore, nil, store, &cfg, nil, duties)

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
	vstore := &mockValidatorStore{
		participatingCommittees: []*registrystorage.Committee{},
	}
	collector := New(zap.NewNop(), vstore, nil, store, &cfg, nil, dutystore.New())

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
	vstore := &mockValidatorStore{
		participatingCommittees: []*registrystorage.Committee{},
	}
	collector := New(zap.NewNop(), vstore, nil, store, &cfg, nil, duties)

	err := collector.computeAndPersistScheduleForSlot(slot)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestCollectorComputeAndPersistScheduleWithCommitteeLinks(t *testing.T) {
	t.Parallel()

	cfg := *networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(96)
	epoch := cfg.EstimatedEpochAtSlot(slot)

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
		{
			Slot:           slot,
			ValidatorIndex: 5,
			Duty: &eth2apiv1.AttesterDuty{
				Slot:           slot,
				ValidatorIndex: 5,
			},
			InCommittee: true,
		},
	})

	store := &mockDutyTraceStore{}

	var committeeID1 spectypes.CommitteeID
	committeeID1[0] = 1
	var committeeID2 spectypes.CommitteeID
	committeeID2[0] = 2

	vstore := &mockValidatorStore{
		participatingCommittees: []*registrystorage.Committee{
			{
				ID:      committeeID1,
				Indices: []phase0.ValidatorIndex{3, 7}, // Validator 3 has duty, 7 doesn't
			},
			{
				ID:      committeeID2,
				Indices: []phase0.ValidatorIndex{5}, // Validator 5 has duty
			},
		},
	}

	collector := New(zap.NewNop(), vstore, nil, store, &cfg, nil, duties)

	err := collector.computeAndPersistScheduleForSlot(slot)
	require.NoError(t, err)

	saved := store.scheduled[slot]
	require.NotNil(t, saved)
	assert.Equal(t, rolemask.BitAttester, saved[3]&rolemask.BitAttester)
	assert.Equal(t, rolemask.BitAttester, saved[5]&rolemask.BitAttester)

	slotToCommittee, found := collector.validatorIndexToCommitteeLinks.Get(3)
	require.True(t, found, "validator 3 should have committee link")
	cid, found := slotToCommittee.Get(slot)
	require.True(t, found, "validator 3 should have link for this slot")
	assert.Equal(t, committeeID1, cid)

	slotToCommittee, found = collector.validatorIndexToCommitteeLinks.Get(5)
	require.True(t, found, "validator 5 should have committee link")
	cid, found = slotToCommittee.Get(slot)
	require.True(t, found, "validator 5 should have link for this slot")
	assert.Equal(t, committeeID2, cid)

	// Validator 7 is in committee but has no duty, so should NOT have a link
	_, found = collector.validatorIndexToCommitteeLinks.Get(7)
	assert.False(t, found, "validator 7 should not have committee link (no duty)")
}

func TestCollectorComputeAndPersistScheduleWithEmptyCommittee(t *testing.T) {
	t.Parallel()

	cfg := *networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(96)
	epoch := cfg.EstimatedEpochAtSlot(slot)

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

	store := &mockDutyTraceStore{}

	var committeeID1 spectypes.CommitteeID
	committeeID1[0] = 1

	vstore := &mockValidatorStore{
		participatingCommittees: []*registrystorage.Committee{
			{
				ID:      committeeID1,
				Indices: []phase0.ValidatorIndex{}, // Empty!
			},
		},
	}

	collector := New(zap.NewNop(), vstore, nil, store, &cfg, nil, duties)

	err := collector.computeAndPersistScheduleForSlot(slot)
	require.NoError(t, err)

	saved := store.scheduled[slot]
	require.NotNil(t, saved)
	assert.Equal(t, rolemask.BitAttester, saved[3]&rolemask.BitAttester)

	linkCount := 0
	collector.validatorIndexToCommitteeLinks.Range(func(_ phase0.ValidatorIndex, _ *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		linkCount++
		return true
	})
	assert.Equal(t, 0, linkCount, "no links should be saved for empty committee")
}
