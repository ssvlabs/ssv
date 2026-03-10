package auditor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	regmocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

type fakeTraceReader struct {
	duties    map[phase0.Slot][]*exporter.CommitteeDutyTrace
	scheduled map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask
	links     map[phase0.Slot][]*exporter.CommitteeDutyLink
}

func (f *fakeTraceReader) GetCommitteeDuties(slot phase0.Slot, _ ...spectypes.BeaconRole) ([]*exporter.CommitteeDutyTrace, error) {
	return f.duties[slot], nil
}

func (f *fakeTraceReader) GetScheduled(slot phase0.Slot) (map[phase0.ValidatorIndex]rolemask.Mask, error) {
	if m, ok := f.scheduled[slot]; ok {
		return m, nil
	}
	return map[phase0.ValidatorIndex]rolemask.Mask{}, nil
}

func (f *fakeTraceReader) GetCommitteeDutyLinks(slot phase0.Slot) ([]*exporter.CommitteeDutyLink, error) {
	return f.links[slot], nil
}

type memStore struct {
	mu       sync.Mutex
	findings []*Finding
}

func (m *memStore) PutFinding(f *Finding) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.findings = append(m.findings, f)
	return true, nil
}
func (m *memStore) Query(q Query) (QueryResult, error) { return QueryResult{}, nil }
func (m *memStore) Prune(_ phase0.Slot) error          { return nil }

type fakeBeacon struct {
	attester map[phase0.ValidatorIndex]*eth2apiv1.AttesterDuty
	sync     map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty
	err      error
}

func (b *fakeBeacon) AttesterDuties(_ context.Context, _ phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
	if b.err != nil {
		return nil, b.err
	}
	out := make([]*eth2apiv1.AttesterDuty, 0, len(indices))
	for _, idx := range indices {
		if d, ok := b.attester[idx]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func (b *fakeBeacon) SyncCommitteeDuties(_ context.Context, _ phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
	if b.err != nil {
		return nil, b.err
	}
	out := make([]*eth2apiv1.SyncCommitteeDuty, 0, len(indices))
	for _, idx := range indices {
		if d, ok := b.sync[idx]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func TestAuditor_ScheduleMissingIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := regmocks.NewMockValidatorStore(ctrl)

	cfg := networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(64)
	epoch := cfg.EstimatedEpochAtSlot(slot)

	var committeeID spectypes.CommitteeID
	committeeID[0] = 0x01

	index := phase0.ValidatorIndex(11)

	// Registry: validator participates in this committee.
	reg.EXPECT().ParticipatingCommittees(epoch).Return([]*registrystorage.Committee{
		{ID: committeeID, Indices: []phase0.ValidatorIndex{index}},
	}).AnyTimes()
	share := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share.ValidatorIndex = index
	reg.EXPECT().ValidatorByIndex(index).Return(share, true).AnyTimes()

	// Duty store expects attester duty at this slot.
	ds := dutystore.New()
	ds.Attester.Set(epoch, []dutystore.StoreDuty[eth2apiv1.AttesterDuty]{
		{
			Slot:           slot,
			ValidatorIndex: index,
			Duty:           &eth2apiv1.AttesterDuty{Slot: slot, ValidatorIndex: index},
			InCommittee:    true,
		},
	})

	// Observed trace includes index, but persisted schedule is missing it (schedule is non-empty).
	tr := &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		Attester: []*exporter.SignerData{{
			Signer:       1,
			ValidatorIdx: []phase0.ValidatorIndex{index},
			ReceivedTime: uint64(time.Now().UnixMilli()),
		}},
	}

	persisted := map[phase0.ValidatorIndex]rolemask.Mask{
		phase0.ValidatorIndex(99): rolemask.BitAttester,
	}

	reader := &fakeTraceReader{
		duties:    map[phase0.Slot][]*exporter.CommitteeDutyTrace{slot: {tr}},
		scheduled: map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask{slot: persisted},
		links:     map[phase0.Slot][]*exporter.CommitteeDutyLink{slot: {{ValidatorIndex: index, CommitteeID: committeeID}}},
	}

	store := &memStore{}
	a := New(zap.NewNop(), cfg, reader, ds, reg, &fakeBeacon{}, store, Options{Enabled: true, RPCFallback: true, DelaySlots: 4})
	require.NoError(t, a.AuditSlot(context.Background(), slot))

	require.NotEmpty(t, store.findings)
	require.Equal(t, ReasonScheduleMissingIndex, store.findings[0].Reason)
	require.True(t, store.findings[0].Evidence.Expected.RPCFallback.Enabled)
	require.False(t, store.findings[0].Evidence.Expected.RPCFallback.Used)
	require.Nil(t, store.findings[0].Evidence.Expected.RPCFallback.OK)
}

func TestAuditor_CommitteeLinkMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := regmocks.NewMockValidatorStore(ctrl)

	cfg := networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(65)
	epoch := cfg.EstimatedEpochAtSlot(slot)
	period := cfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)

	var committeeID spectypes.CommitteeID
	committeeID[0] = 0x02
	index := phase0.ValidatorIndex(22)

	reg.EXPECT().ParticipatingCommittees(epoch).Return([]*registrystorage.Committee{
		{ID: committeeID, Indices: []phase0.ValidatorIndex{index}},
	}).AnyTimes()
	share := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share.ValidatorIndex = index
	reg.EXPECT().ValidatorByIndex(index).Return(share, true).AnyTimes()

	ds := dutystore.New()
	ds.SyncCommittee.Set(period, []dutystore.StoreSyncCommitteeDuty{
		{ValidatorIndex: index, Duty: &eth2apiv1.SyncCommitteeDuty{ValidatorIndex: index}, InCommittee: true},
	})

	tr := &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		SyncCommittee: []*exporter.SignerData{{
			Signer:       2,
			ValidatorIdx: []phase0.ValidatorIndex{index},
			ReceivedTime: uint64(time.Now().UnixMilli()),
		}},
	}

	persisted := map[phase0.ValidatorIndex]rolemask.Mask{
		index: rolemask.BitSyncCommittee,
	}

	reader := &fakeTraceReader{
		duties:    map[phase0.Slot][]*exporter.CommitteeDutyTrace{slot: {tr}},
		scheduled: map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask{slot: persisted},
		links:     map[phase0.Slot][]*exporter.CommitteeDutyLink{slot: {}}, // missing
	}

	store := &memStore{}
	a := New(zap.NewNop(), cfg, reader, ds, reg, &fakeBeacon{}, store, Options{Enabled: true, RPCFallback: false, DelaySlots: 4})
	require.NoError(t, a.AuditSlot(context.Background(), slot))

	require.NotEmpty(t, store.findings)
	require.Equal(t, ReasonCommitteeLinkMissing, store.findings[0].Reason)
}

func TestAuditor_DutyStoreIncomplete_RPCConfirms(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := regmocks.NewMockValidatorStore(ctrl)

	cfg := networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(96)
	epoch := cfg.EstimatedEpochAtSlot(slot)

	var committeeID spectypes.CommitteeID
	committeeID[0] = 0x03
	index := phase0.ValidatorIndex(33)

	reg.EXPECT().ParticipatingCommittees(epoch).Return([]*registrystorage.Committee{
		{ID: committeeID, Indices: []phase0.ValidatorIndex{index}},
	}).AnyTimes()
	share := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share.ValidatorIndex = index
	reg.EXPECT().ValidatorByIndex(index).Return(share, true).AnyTimes()

	ds := dutystore.New() // empty => duty store doesn't expect it

	tr := &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		Attester: []*exporter.SignerData{{
			Signer:       3,
			ValidatorIdx: []phase0.ValidatorIndex{index},
			ReceivedTime: uint64(time.Now().UnixMilli()),
		}},
	}

	reader := &fakeTraceReader{
		duties: map[phase0.Slot][]*exporter.CommitteeDutyTrace{slot: {tr}},
		scheduled: map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask{slot: {
			phase0.ValidatorIndex(99): rolemask.BitAttester,
		}},
		links: map[phase0.Slot][]*exporter.CommitteeDutyLink{slot: {{ValidatorIndex: index, CommitteeID: committeeID}}},
	}

	beacon := &fakeBeacon{
		attester: map[phase0.ValidatorIndex]*eth2apiv1.AttesterDuty{
			index: {Slot: slot, ValidatorIndex: index},
		},
	}

	store := &memStore{}
	a := New(zap.NewNop(), cfg, reader, ds, reg, beacon, store, Options{Enabled: true, RPCFallback: true, DelaySlots: 4})
	require.NoError(t, a.AuditSlot(context.Background(), slot))
	require.NotEmpty(t, store.findings)
	require.Equal(t, ReasonDutyStoreIncomplete, store.findings[0].Reason)
	require.True(t, store.findings[0].Evidence.Expected.RPCFallback.Used)
	require.NotNil(t, store.findings[0].Evidence.Expected.RPCFallback.OK)
	require.True(t, *store.findings[0].Evidence.Expected.RPCFallback.OK)
}

func TestAuditor_DutyFetchFailed_IsAttributed(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := regmocks.NewMockValidatorStore(ctrl)

	cfg := networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(128)
	epoch := cfg.EstimatedEpochAtSlot(slot)

	var committeeID spectypes.CommitteeID
	committeeID[0] = 0x04
	index := phase0.ValidatorIndex(44)

	reg.EXPECT().ParticipatingCommittees(epoch).Return([]*registrystorage.Committee{
		{ID: committeeID, Indices: []phase0.ValidatorIndex{index}},
	}).AnyTimes()
	share := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share.ValidatorIndex = index
	reg.EXPECT().ValidatorByIndex(index).Return(share, true).AnyTimes()

	ds := dutystore.New()

	tr := &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		Attester: []*exporter.SignerData{{
			Signer:       4,
			ValidatorIdx: []phase0.ValidatorIndex{index},
			ReceivedTime: uint64(time.Now().UnixMilli()),
		}},
	}

	reader := &fakeTraceReader{
		duties: map[phase0.Slot][]*exporter.CommitteeDutyTrace{slot: {tr}},
		scheduled: map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask{slot: {
			phase0.ValidatorIndex(99): rolemask.BitAttester,
		}},
		links: map[phase0.Slot][]*exporter.CommitteeDutyLink{slot: {{ValidatorIndex: index, CommitteeID: committeeID}}},
	}

	store := &memStore{}
	a := New(zap.NewNop(), cfg, reader, ds, reg, &fakeBeacon{}, store, Options{Enabled: true, RPCFallback: true, DelaySlots: 4})
	ep := epoch
	a.RecordDutyFetch(DutyFetchEvent{
		Role:      spectypes.BNRoleAttester,
		Epoch:     &ep,
		At:        time.Now().Add(-time.Second),
		Took:      10 * time.Millisecond,
		Requested: 100,
		Returned:  0,
		Err:       errors.New("context deadline exceeded"),
	})

	require.NoError(t, a.AuditSlot(context.Background(), slot))
	require.NotEmpty(t, store.findings)
	require.Equal(t, ReasonDutyFetchFailed, store.findings[0].Reason)
}

func TestAuditor_RPCFallbackFailed(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := regmocks.NewMockValidatorStore(ctrl)

	cfg := networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(160)
	epoch := cfg.EstimatedEpochAtSlot(slot)

	var committeeID spectypes.CommitteeID
	committeeID[0] = 0x05
	index := phase0.ValidatorIndex(55)

	reg.EXPECT().ParticipatingCommittees(epoch).Return([]*registrystorage.Committee{
		{ID: committeeID, Indices: []phase0.ValidatorIndex{index}},
	}).AnyTimes()
	share := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share.ValidatorIndex = index
	reg.EXPECT().ValidatorByIndex(index).Return(share, true).AnyTimes()

	ds := dutystore.New() // empty -> forces fallback

	tr := &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		Attester: []*exporter.SignerData{{
			Signer:       5,
			ValidatorIdx: []phase0.ValidatorIndex{index},
			ReceivedTime: uint64(time.Now().UnixMilli()),
		}},
	}

	reader := &fakeTraceReader{
		duties: map[phase0.Slot][]*exporter.CommitteeDutyTrace{slot: {tr}},
		scheduled: map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask{slot: {
			phase0.ValidatorIndex(99): rolemask.BitAttester,
		}},
		links: map[phase0.Slot][]*exporter.CommitteeDutyLink{slot: {{ValidatorIndex: index, CommitteeID: committeeID}}},
	}

	beacon := &fakeBeacon{err: errors.New("rpc timeout")}

	store := &memStore{}
	a := New(zap.NewNop(), cfg, reader, ds, reg, beacon, store, Options{Enabled: true, RPCFallback: true, DelaySlots: 4})
	require.NoError(t, a.AuditSlot(context.Background(), slot))
	require.NotEmpty(t, store.findings)
	require.Equal(t, ReasonRPCFallbackFailed, store.findings[0].Reason)
	require.True(t, store.findings[0].Evidence.Expected.RPCFallback.Used)
	require.NotNil(t, store.findings[0].Evidence.Expected.RPCFallback.OK)
	require.False(t, *store.findings[0].Evidence.Expected.RPCFallback.OK)
}

func TestAuditor_RPCFallbackSkipped_MaxIndices(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := regmocks.NewMockValidatorStore(ctrl)

	cfg := networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(161)
	epoch := cfg.EstimatedEpochAtSlot(slot)

	var committeeID spectypes.CommitteeID
	committeeID[0] = 0x15
	index1 := phase0.ValidatorIndex(155)
	index2 := phase0.ValidatorIndex(156)

	reg.EXPECT().ParticipatingCommittees(epoch).Return([]*registrystorage.Committee{
		{ID: committeeID, Indices: []phase0.ValidatorIndex{index1, index2}},
	}).AnyTimes()
	share := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share1 := share
	share1.ValidatorIndex = index1
	reg.EXPECT().ValidatorByIndex(index1).Return(share1, true).AnyTimes()
	share2 := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share2.ValidatorIndex = index2
	reg.EXPECT().ValidatorByIndex(index2).Return(share2, true).AnyTimes()

	ds := dutystore.New() // empty -> would request RPC, but cap is 1

	tr := &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		Attester: []*exporter.SignerData{{
			Signer:       15,
			ValidatorIdx: []phase0.ValidatorIndex{index1, index2},
			ReceivedTime: uint64(time.Now().UnixMilli()),
		}},
	}

	reader := &fakeTraceReader{
		duties: map[phase0.Slot][]*exporter.CommitteeDutyTrace{slot: {tr}},
		scheduled: map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask{slot: {
			phase0.ValidatorIndex(99): rolemask.BitAttester,
		}},
		links: map[phase0.Slot][]*exporter.CommitteeDutyLink{slot: {
			{ValidatorIndex: index1, CommitteeID: committeeID},
			{ValidatorIndex: index2, CommitteeID: committeeID},
		}},
	}

	store := &memStore{}
	a := New(zap.NewNop(), cfg, reader, ds, reg, &fakeBeacon{}, store, Options{Enabled: true, RPCFallback: true, RPCMaxIndicesPerSlot: 1, DelaySlots: 4})
	require.NoError(t, a.AuditSlot(context.Background(), slot))
	require.NotEmpty(t, store.findings)

	var skipped *Finding
	for _, f := range store.findings {
		if f.Reason == ReasonRPCFallbackSkipped {
			skipped = f
			break
		}
	}
	require.NotNil(t, skipped)
	require.True(t, skipped.Evidence.Expected.RPCFallback.Enabled)
	require.False(t, skipped.Evidence.Expected.RPCFallback.Used)
	require.Nil(t, skipped.Evidence.Expected.RPCFallback.OK)
}

func TestAuditor_ScheduleJobDropped_IsAttributed(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := regmocks.NewMockValidatorStore(ctrl)

	cfg := networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(192)
	epoch := cfg.EstimatedEpochAtSlot(slot)

	var committeeID spectypes.CommitteeID
	committeeID[0] = 0x06
	index := phase0.ValidatorIndex(66)

	reg.EXPECT().ParticipatingCommittees(epoch).Return([]*registrystorage.Committee{
		{ID: committeeID, Indices: []phase0.ValidatorIndex{index}},
	}).AnyTimes()
	share := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share.ValidatorIndex = index
	reg.EXPECT().ValidatorByIndex(index).Return(share, true).AnyTimes()

	ds := dutystore.New()
	ds.Attester.Set(epoch, []dutystore.StoreDuty[eth2apiv1.AttesterDuty]{
		{
			Slot:           slot,
			ValidatorIndex: index,
			Duty:           &eth2apiv1.AttesterDuty{Slot: slot, ValidatorIndex: index},
			InCommittee:    true,
		},
	})

	tr := &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		Attester: []*exporter.SignerData{{
			Signer:       6,
			ValidatorIdx: []phase0.ValidatorIndex{index},
			ReceivedTime: uint64(time.Now().UnixMilli()),
		}},
	}

	reader := &fakeTraceReader{
		duties: map[phase0.Slot][]*exporter.CommitteeDutyTrace{slot: {tr}},
		scheduled: map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask{slot: {
			phase0.ValidatorIndex(99): rolemask.BitAttester,
		}},
		links: map[phase0.Slot][]*exporter.CommitteeDutyLink{slot: {{ValidatorIndex: index, CommitteeID: committeeID}}},
	}

	store := &memStore{}
	a := New(zap.NewNop(), cfg, reader, ds, reg, &fakeBeacon{}, store, Options{Enabled: true, RPCFallback: true, DelaySlots: 4})
	a.RecordScheduleJobDropped(slot)

	require.NoError(t, a.AuditSlot(context.Background(), slot))
	require.NotEmpty(t, store.findings)
	require.Equal(t, ReasonScheduleJobDropped, store.findings[0].Reason)
}

func TestAuditor_ScheduleComputeFailed_IsAttributed(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := regmocks.NewMockValidatorStore(ctrl)

	cfg := networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(224)
	epoch := cfg.EstimatedEpochAtSlot(slot)

	var committeeID spectypes.CommitteeID
	committeeID[0] = 0x07
	index := phase0.ValidatorIndex(77)

	reg.EXPECT().ParticipatingCommittees(epoch).Return([]*registrystorage.Committee{
		{ID: committeeID, Indices: []phase0.ValidatorIndex{index}},
	}).AnyTimes()
	share := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share.ValidatorIndex = index
	reg.EXPECT().ValidatorByIndex(index).Return(share, true).AnyTimes()

	ds := dutystore.New()
	ds.Attester.Set(epoch, []dutystore.StoreDuty[eth2apiv1.AttesterDuty]{
		{
			Slot:           slot,
			ValidatorIndex: index,
			Duty:           &eth2apiv1.AttesterDuty{Slot: slot, ValidatorIndex: index},
			InCommittee:    true,
		},
	})

	tr := &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		Attester: []*exporter.SignerData{{
			Signer:       7,
			ValidatorIdx: []phase0.ValidatorIndex{index},
			ReceivedTime: uint64(time.Now().UnixMilli()),
		}},
	}

	reader := &fakeTraceReader{
		duties: map[phase0.Slot][]*exporter.CommitteeDutyTrace{slot: {tr}},
		scheduled: map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask{slot: {
			phase0.ValidatorIndex(99): rolemask.BitAttester,
		}},
		links: map[phase0.Slot][]*exporter.CommitteeDutyLink{slot: {{ValidatorIndex: index, CommitteeID: committeeID}}},
	}

	store := &memStore{}
	a := New(zap.NewNop(), cfg, reader, ds, reg, &fakeBeacon{}, store, Options{Enabled: true, RPCFallback: true, DelaySlots: 4})
	a.RecordScheduleCompute(ScheduleComputeEvent{
		Slot: slot,
		At:   time.Now().UTC(),
		Took: 5 * time.Millisecond,
		Size: 0,
		Err:  errors.New("db write failed"),
	})

	require.NoError(t, a.AuditSlot(context.Background(), slot))
	require.NotEmpty(t, store.findings)
	require.Equal(t, ReasonScheduleComputeFailed, store.findings[0].Reason)
}

func TestAuditor_ScheduleBeforeDutiesReady_IsAttributed(t *testing.T) {
	ctrl := gomock.NewController(t)
	reg := regmocks.NewMockValidatorStore(ctrl)

	cfg := networkconfig.TestNetwork.Beacon
	slot := phase0.Slot(256)
	epoch := cfg.EstimatedEpochAtSlot(slot)

	var committeeID spectypes.CommitteeID
	committeeID[0] = 0x08
	index := phase0.ValidatorIndex(88)

	reg.EXPECT().ParticipatingCommittees(epoch).Return([]*registrystorage.Committee{
		{ID: committeeID, Indices: []phase0.ValidatorIndex{index}},
	}).AnyTimes()
	share := &ssvtypes.SSVShare{Status: eth2apiv1.ValidatorStateActiveOngoing}
	share.ValidatorIndex = index
	reg.EXPECT().ValidatorByIndex(index).Return(share, true).AnyTimes()

	ds := dutystore.New()
	ds.Attester.Set(epoch, []dutystore.StoreDuty[eth2apiv1.AttesterDuty]{
		{
			Slot:           slot,
			ValidatorIndex: index,
			Duty:           &eth2apiv1.AttesterDuty{Slot: slot, ValidatorIndex: index},
			InCommittee:    true,
		},
	})

	tr := &exporter.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		Attester: []*exporter.SignerData{{
			Signer:       8,
			ValidatorIdx: []phase0.ValidatorIndex{index},
			ReceivedTime: uint64(time.Now().UnixMilli()),
		}},
	}

	reader := &fakeTraceReader{
		duties: map[phase0.Slot][]*exporter.CommitteeDutyTrace{slot: {tr}},
		scheduled: map[phase0.Slot]map[phase0.ValidatorIndex]rolemask.Mask{slot: {
			phase0.ValidatorIndex(99): rolemask.BitAttester,
		}},
		links: map[phase0.Slot][]*exporter.CommitteeDutyLink{slot: {{ValidatorIndex: index, CommitteeID: committeeID}}},
	}

	store := &memStore{}
	a := New(zap.NewNop(), cfg, reader, ds, reg, &fakeBeacon{}, store, Options{Enabled: true, RPCFallback: true, DelaySlots: 4})

	// Duty fetch completes in the future relative to schedule compute.
	ep := epoch
	fetchAt := time.Now().UTC()
	a.RecordDutyFetch(DutyFetchEvent{
		Role:      spectypes.BNRoleAttester,
		Epoch:     &ep,
		At:        fetchAt,
		Took:      500 * time.Millisecond,
		Requested: 100,
		Returned:  100,
		Err:       nil,
	})
	a.RecordScheduleCompute(ScheduleComputeEvent{
		Slot: slot,
		At:   fetchAt.Add(100 * time.Millisecond), // before duties readyAt (fetchAt+500ms)
		Took: 2 * time.Millisecond,
		Size: 1,
		Err:  nil,
	})

	require.NoError(t, a.AuditSlot(context.Background(), slot))
	require.NotEmpty(t, store.findings)
	require.Equal(t, ReasonScheduleBeforeDutiesReady, store.findings[0].Reason)
}
