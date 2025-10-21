package goclient

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/mock"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
)

// committeeClientMock overrides BeaconCommittees to simplify instrumentation in tests.
type committeeClientMock struct {
	mock.Service
	resp  []*eth2apiv1.BeaconCommittee
	err   error
	calls int
}

func (s *committeeClientMock) BeaconCommittees(ctx context.Context, opts *api.BeaconCommitteesOpts) (*api.Response[[]*eth2apiv1.BeaconCommittee], error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &api.Response[[]*eth2apiv1.BeaconCommittee]{
		Data: s.resp,
	}, nil
}

// Override signature differences between mock.Service and the MultiClient interface used by GoClient.
func (s *committeeClientMock) SubmitProposal(context.Context, *api.SubmitProposalOpts) error {
	return nil
}

func newRunningCache() *ttlcache.Cache[phase0.Epoch, []*eth2apiv1.BeaconCommittee] {
	cache := ttlcache.New(ttlcache.WithTTL[phase0.Epoch, []*eth2apiv1.BeaconCommittee](time.Minute))
	go cache.Start()
	return cache
}

func TestCommitteesForEpochCachesResults(t *testing.T) {
	t.Parallel()

	service := &committeeClientMock{resp: []*eth2apiv1.BeaconCommittee{{Slot: 64, Index: 1}}}
	cache := newRunningCache()
	defer cache.Stop()

	client := &GoClient{
		log:             zap.NewNop(),
		multiClient:     service,
		committeesCache: cache,
		commonTimeout:   time.Second,
	}

	ctx := context.Background()
	epoch := phase0.Epoch(2)

	first, err := client.CommitteesForEpoch(ctx, epoch)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, 1, service.calls)

	second, err := client.CommitteesForEpoch(ctx, epoch)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, 1, service.calls, "cached result should avoid second upstream call")
}

func TestCommitteesForSlotFiltersBySlot(t *testing.T) {
	t.Parallel()

	targetSlot := phase0.Slot(128)
	service := &committeeClientMock{
		resp: []*eth2apiv1.BeaconCommittee{
			{Slot: targetSlot, Index: 1},
			{Slot: targetSlot + 1, Index: 2},
			nil,
		},
	}

	cache := newRunningCache()
	defer cache.Stop()

	cfg := *networkconfig.TestNetwork.Beacon
	client := &GoClient{
		log:             zap.NewNop(),
		multiClient:     service,
		committeesCache: cache,
		commonTimeout:   time.Second,
	}
	client.beaconConfig = &cfg

	results, err := client.CommitteesForSlot(context.Background(), targetSlot)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, targetSlot, results[0].Slot)
	assert.Equal(t, 1, service.calls)

	// Second invocation hits the cache (CommitteesForEpoch) and should not increase call count.
	results, err = client.CommitteesForSlot(context.Background(), targetSlot)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 1, service.calls)
}
