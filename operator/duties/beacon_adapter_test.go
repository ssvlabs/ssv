package duties

import (
	"bytes"
	"context"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	beaconmock "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

// committeeAwareMock augments the generated BeaconNode mock with
// CommitteesForEpoch instrumentation so prefetchingBeacon can exercise its
// optimisation paths in tests.
type committeeAwareMock struct {
	*beaconmock.MockBeaconNode

	committees      []*eth2apiv1.BeaconCommittee
	committeesErr   error
	committeesCalls int
}

func (m *committeeAwareMock) CommitteesForEpoch(context.Context, phase0.Epoch) ([]*eth2apiv1.BeaconCommittee, error) {
	m.committeesCalls++
	return m.committees, m.committeesErr
}

// for simplicity, validatorStoreStub is a focused ValidatorStore implementation that only
// supports ValidatorPubkey, as required by ensureAttesterEpoch.
type validatorStoreStub struct {
	mapping map[phase0.ValidatorIndex]spectypes.ValidatorPK
}

func newValidatorStoreStub(entries map[phase0.ValidatorIndex]spectypes.ValidatorPK) *validatorStoreStub {
	return &validatorStoreStub{mapping: entries}
}

func (v *validatorStoreStub) ValidatorPubkey(index phase0.ValidatorIndex) (spectypes.ValidatorPK, bool) {
	pk, ok := v.mapping[index]
	return pk, ok
}

func TestPrefetchingBeaconCommitteesCaching(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	committees := []*eth2apiv1.BeaconCommittee{{Slot: 64, Index: 1}}
	node := &committeeAwareMock{
		MockBeaconNode: beaconmock.NewMockBeaconNode(ctrl),
		committees:     committees,
	}

	cfg := *networkconfig.TestNetwork.Beacon
	pb := NewPrefetchingBeacon(zap.NewNop(), node, &cfg, nil)

	epoch := cfg.EstimatedEpochAtSlot(committees[0].Slot)

	first, err := pb.committeesForEpoch(context.Background(), epoch)
	require.NoError(t, err)
	require.Equal(t, committees, first)
	require.Equal(t, 1, node.committeesCalls)

	second, err := pb.committeesForEpoch(context.Background(), epoch)
	require.NoError(t, err)
	require.Equal(t, committees, second)
	assert.Equal(t, 1, node.committeesCalls, "cached result should avoid additional upstream calls")
}

func TestPrefetchingBeaconEnsureAttesterEpoch(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	slot := phase0.Slot(96)
	cfg := *networkconfig.TestNetwork.Beacon
	epoch := cfg.EstimatedEpochAtSlot(slot)
	committees := []*eth2apiv1.BeaconCommittee{{Slot: slot, Index: 2, Validators: []phase0.ValidatorIndex{1, 2}}}

	node := &committeeAwareMock{
		MockBeaconNode: beaconmock.NewMockBeaconNode(ctrl),
		committees:     committees,
	}

	var pk spectypes.ValidatorPK
	copy(pk[:], bytes.Repeat([]byte{0x01}, len(pk)))
	vstore := newValidatorStoreStub(map[phase0.ValidatorIndex]spectypes.ValidatorPK{1: pk})

	pb := NewPrefetchingBeacon(zap.NewNop(), node, &cfg, vstore)

	require.NoError(t, pb.ensureAttesterEpoch(context.Background(), epoch))

	pb.muAtt.RLock()
	duties := pb.attester[epoch]
	pb.muAtt.RUnlock()

	require.NotNil(t, duties)
	duty, ok := duties[1]
	require.True(t, ok)
	assert.Equal(t, slot, duty.Slot)
	var got spectypes.ValidatorPK
	copy(got[:], duty.PubKey[:])
	assert.Equal(t, pk, got)
	assert.EqualValues(t, 2, duty.CommitteeIndex)
	assert.EqualValues(t, len(committees[0].Validators), int(duty.CommitteeLength))
}

func TestPrefetchingBeaconEnsureProposerEpochBatchesIndices(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	node := beaconmock.NewMockBeaconNode(ctrl)

	var requested []phase0.ValidatorIndex
	node.EXPECT().ProposerDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
		requested = append([]phase0.ValidatorIndex(nil), indices...)
		return []*eth2apiv1.ProposerDuty{{ValidatorIndex: 5}}, nil
	}).Times(1)

	cfg := *networkconfig.TestNetwork.Beacon
	pb := NewPrefetchingBeacon(zap.NewNop(), &committeeAwareMock{MockBeaconNode: node}, &cfg, nil)

	indices := []phase0.ValidatorIndex{1, 2, 3}
	require.NoError(t, pb.ensureProposerEpoch(context.Background(), 0, indices))
	assert.ElementsMatch(t, indices, requested)

	pb.muProp.RLock()
	defer pb.muProp.RUnlock()
	assert.NotNil(t, pb.proposer[0])
}

func TestPrefetchingBeaconEnsureSyncPeriod(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	node := beaconmock.NewMockBeaconNode(ctrl)
	node.EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ phase0.Epoch, _ []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
		return []*eth2apiv1.SyncCommitteeDuty{{ValidatorIndex: 7}}, nil
	}).Times(1)

	cfg := *networkconfig.TestNetwork.Beacon
	pb := NewPrefetchingBeacon(zap.NewNop(), &committeeAwareMock{MockBeaconNode: node}, &cfg, nil)

	indices := []phase0.ValidatorIndex{7}
	require.NoError(t, pb.ensureSyncPeriod(context.Background(), 0, indices))

	pb.muSync.RLock()
	defer pb.muSync.RUnlock()
	period := cfg.EstimatedSyncCommitteePeriodAtEpoch(0)
	duties := pb.sync[period]
	require.NotNil(t, duties)
	require.NotNil(t, duties[7])
}

func TestPrefetchingBeaconCommitteesUnsupported(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	node := beaconmock.NewMockBeaconNode(ctrl)
	cfg := *networkconfig.TestNetwork.Beacon
	pb := NewPrefetchingBeacon(zap.NewNop(), node, &cfg, nil)

	_, err := pb.committeesForEpoch(context.Background(), 0)
	assert.ErrorIs(t, err, ErrCommitteesUnsupported)
}
