package goclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/mock"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
)

type syncCommitteeClientMock struct {
	mock.Service

	syncCommitteeDutiesFunc       func(context.Context, *api.SyncCommitteeDutiesOpts) (*api.Response[[]*eth2apiv1.SyncCommitteeDuty], error)
	submitSyncCommitteeMessagesFn func(context.Context, []*altair.SyncCommitteeMessage) error
	beaconBlockRootFunc           func(context.Context, *api.BeaconBlockRootOpts) (*api.Response[*phase0.Root], error)
	syncCommitteeContributionFunc func(context.Context, *api.SyncCommitteeContributionOpts) (*api.Response[*altair.SyncCommitteeContribution], error)
}

func (s *syncCommitteeClientMock) SyncCommitteeDuties(ctx context.Context, opts *api.SyncCommitteeDutiesOpts) (*api.Response[[]*eth2apiv1.SyncCommitteeDuty], error) {
	if s.syncCommitteeDutiesFunc != nil {
		return s.syncCommitteeDutiesFunc(ctx, opts)
	}
	return s.Service.SyncCommitteeDuties(ctx, opts)
}

func (s *syncCommitteeClientMock) SubmitSyncCommitteeMessages(ctx context.Context, msgs []*altair.SyncCommitteeMessage) error {
	if s.submitSyncCommitteeMessagesFn != nil {
		return s.submitSyncCommitteeMessagesFn(ctx, msgs)
	}
	return s.Service.SubmitSyncCommitteeMessages(ctx, msgs)
}

func (s *syncCommitteeClientMock) BeaconBlockRoot(ctx context.Context, opts *api.BeaconBlockRootOpts) (*api.Response[*phase0.Root], error) {
	if s.beaconBlockRootFunc != nil {
		return s.beaconBlockRootFunc(ctx, opts)
	}
	return s.Service.BeaconBlockRoot(ctx, opts)
}

func (s *syncCommitteeClientMock) SyncCommitteeContribution(ctx context.Context, opts *api.SyncCommitteeContributionOpts) (*api.Response[*altair.SyncCommitteeContribution], error) {
	if s.syncCommitteeContributionFunc != nil {
		return s.syncCommitteeContributionFunc(ctx, opts)
	}
	return s.Service.SyncCommitteeContribution(ctx, opts)
}

func (s *syncCommitteeClientMock) SubmitProposal(context.Context, *api.SubmitProposalOpts) error {
	return nil
}

func (s *syncCommitteeClientMock) NodeClient(context.Context) (*api.Response[string], error) {
	return &api.Response[string]{Data: "mock"}, nil
}

func (s *syncCommitteeClientMock) SubmitBlindedProposal(context.Context, *api.SubmitBlindedProposalOpts) error {
	return nil
}

func TestSyncCommitteeDuties(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		response       *api.Response[[]*eth2apiv1.SyncCommitteeDuty]
		err            error
		expectedErr    string
		expectedDuties []*eth2apiv1.SyncCommitteeDuty
	}{
		{
			name: "success",
			response: &api.Response[[]*eth2apiv1.SyncCommitteeDuty]{
				Data: []*eth2apiv1.SyncCommitteeDuty{
					{ValidatorIndex: 11, ValidatorSyncCommitteeIndices: []phase0.CommitteeIndex{2, 3}},
				},
			},
			expectedDuties: []*eth2apiv1.SyncCommitteeDuty{
				{ValidatorIndex: 11, ValidatorSyncCommitteeIndices: []phase0.CommitteeIndex{2, 3}},
			},
		},
		{
			name:        "upstream error",
			err:         errors.New("boom"),
			expectedErr: "fetch sync committee duties",
		},
		{
			name:        "nil response",
			expectedErr: "sync committee duties response is nil",
		},
		{
			name:        "nil data",
			response:    &api.Response[[]*eth2apiv1.SyncCommitteeDuty]{},
			expectedErr: "sync committee duties response data is nil",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var receivedOpts *api.SyncCommitteeDutiesOpts
			client := &syncCommitteeClientMock{
				syncCommitteeDutiesFunc: func(_ context.Context, opts *api.SyncCommitteeDutiesOpts) (*api.Response[[]*eth2apiv1.SyncCommitteeDuty], error) {
					receivedOpts = opts
					return tc.response, tc.err
				},
			}

			goClient := &GoClient{
				log:         zap.NewNop(),
				multiClient: client,
			}

			indices := []phase0.ValidatorIndex{1, 5}
			duties, err := goClient.SyncCommitteeDuties(t.Context(), 9, indices)

			require.NotNil(t, receivedOpts)
			require.Equal(t, phase0.Epoch(9), receivedOpts.Epoch)
			require.Equal(t, indices, receivedOpts.Indices)

			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				require.Nil(t, duties)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedDuties, duties)
		})
	}
}

func TestSubmitSyncMessages(t *testing.T) {
	t.Parallel()

	msgs := []*altair.SyncCommitteeMessage{{Slot: 123, ValidatorIndex: 7}}

	t.Run("single multiclient submission", func(t *testing.T) {
		t.Parallel()

		calls := atomic.Int32{}
		client := &syncCommitteeClientMock{
			submitSyncCommitteeMessagesFn: func(_ context.Context, got []*altair.SyncCommitteeMessage) error {
				calls.Add(1)
				require.Equal(t, msgs, got)
				return nil
			},
		}

		goClient := &GoClient{
			log:         zap.NewNop(),
			multiClient: client,
		}

		require.NoError(t, goClient.SubmitSyncMessages(t.Context(), msgs))
		require.EqualValues(t, 1, calls.Load())
	})

	t.Run("parallel submission succeeds when one client succeeds", func(t *testing.T) {
		t.Parallel()

		var firstCalls atomic.Int32
		var secondCalls atomic.Int32
		first := &syncCommitteeClientMock{
			submitSyncCommitteeMessagesFn: func(_ context.Context, got []*altair.SyncCommitteeMessage) error {
				firstCalls.Add(1)
				require.Equal(t, msgs, got)
				return errors.New("first failed")
			},
		}
		second := &syncCommitteeClientMock{
			submitSyncCommitteeMessagesFn: func(_ context.Context, got []*altair.SyncCommitteeMessage) error {
				secondCalls.Add(1)
				require.Equal(t, msgs, got)
				return nil
			},
		}

		goClient := &GoClient{
			log:                     zap.NewNop(),
			clients:                 []Client{first, second},
			withParallelSubmissions: true,
		}

		require.NoError(t, goClient.SubmitSyncMessages(t.Context(), msgs))
		require.EqualValues(t, 1, firstCalls.Load())
		require.EqualValues(t, 1, secondCalls.Load())
	})

	t.Run("parallel submission fails when all clients fail", func(t *testing.T) {
		t.Parallel()

		first := &syncCommitteeClientMock{
			submitSyncCommitteeMessagesFn: func(context.Context, []*altair.SyncCommitteeMessage) error {
				return errors.New("first failed")
			},
		}
		second := &syncCommitteeClientMock{
			submitSyncCommitteeMessagesFn: func(context.Context, []*altair.SyncCommitteeMessage) error {
				return errors.New("second failed")
			},
		}

		goClient := &GoClient{
			log:                     zap.NewNop(),
			clients:                 []Client{first, second},
			withParallelSubmissions: true,
		}

		err := goClient.SubmitSyncMessages(t.Context(), msgs)
		require.ErrorContains(t, err, "all clients failed to submit")
	})
}

func TestGetSyncCommitteeContributionPreservesInputOrdering(t *testing.T) {
	t.Parallel()

	cfg := *networkconfig.TestNetwork.Beacon
	blockRoot := phase0.Root{1, 2, 3}
	slot := phase0.Slot(64)
	selectionProofs := []phase0.BLSSignature{
		signatureWithFirstByte(1),
		signatureWithFirstByte(2),
		signatureWithFirstByte(3),
	}
	subnetIDs := []uint64{11, 22, 33}

	var (
		mu          sync.Mutex
		requested   []uint64
		started     atomic.Int32
		allStarted  = make(chan struct{})
		allReleased = make(chan struct{})
	)
	client := &syncCommitteeClientMock{
		beaconBlockRootFunc: func(_ context.Context, opts *api.BeaconBlockRootOpts) (*api.Response[*phase0.Root], error) {
			// Must query head, not the duty slot: a by-slot lookup 404s when the slot's
			// proposal is missed, failing the contribution duty.
			require.Equal(t, "head", opts.Block)
			return &api.Response[*phase0.Root]{Data: &blockRoot}, nil
		},
		syncCommitteeContributionFunc: func(_ context.Context, opts *api.SyncCommitteeContributionOpts) (*api.Response[*altair.SyncCommitteeContribution], error) {
			mu.Lock()
			requested = append(requested, opts.SubcommitteeIndex)
			mu.Unlock()

			if started.Add(1) == int32(len(subnetIDs)) {
				close(allStarted)
			}
			<-allStarted
			<-allReleased

			return &api.Response[*altair.SyncCommitteeContribution]{
				Data: &altair.SyncCommitteeContribution{
					Slot:              opts.Slot,
					BeaconBlockRoot:   opts.BeaconBlockRoot,
					SubcommitteeIndex: opts.SubcommitteeIndex,
				},
			}, nil
		},
	}

	goClient := &GoClient{
		log:          zap.NewNop(),
		beaconConfig: &cfg,
		multiClient:  client,
	}

	go func() {
		<-allStarted
		close(allReleased)
	}()

	got, version, err := goClient.GetSyncCommitteeContribution(t.Context(), slot, selectionProofs, subnetIDs)
	require.NoError(t, err)
	require.Equal(t, spec.DataVersionAltair, version)

	contributions, ok := got.(*spectypes.Contributions)
	require.True(t, ok)
	require.Len(t, *contributions, len(subnetIDs))
	require.ElementsMatch(t, subnetIDs, requested)

	for index, contribution := range *contributions {
		require.NotNil(t, contribution)
		require.Equal(t, [96]byte(selectionProofs[index]), contribution.SelectionProofSig)
		require.Equal(t, subnetIDs[index], contribution.Contribution.SubcommitteeIndex)
		require.Equal(t, blockRoot, contribution.Contribution.BeaconBlockRoot)
		require.Equal(t, slot, contribution.Contribution.Slot)
	}
}

// TestGetSyncCommitteeContributionFetchesHeadRoot is a regression test for the
// empty-slot 404 bug: the contribution root must be resolved from "head", never the
// duty slot. When the slot's proposal is missed, a by-slot block-root lookup 404s and
// terminally fails the duty for the whole cluster, whereas "head" resolves to the
// previous block's root — in the common case the root the cluster's sync-committee
// messages signed.
//
// The mock 404s any by-slot lookup and only answers "head", so this test fails if the
// query ever regresses to the by-slot form.
func TestGetSyncCommitteeContributionFetchesHeadRoot(t *testing.T) {
	t.Parallel()

	cfg := *networkconfig.TestNetwork.Beacon
	headRoot := phase0.Root{4, 5, 6}
	slot := phase0.Slot(64)
	selectionProofs := []phase0.BLSSignature{signatureWithFirstByte(1)}
	subnetIDs := []uint64{7}

	client := &syncCommitteeClientMock{
		beaconBlockRootFunc: func(_ context.Context, opts *api.BeaconBlockRootOpts) (*api.Response[*phase0.Root], error) {
			if opts.Block != "head" {
				// Emulate a missed-proposal slot: the by-slot lookup the old code used 404s.
				return nil, errors.New("GET failed with status 404: block not found")
			}
			return &api.Response[*phase0.Root]{Data: &headRoot}, nil
		},
		syncCommitteeContributionFunc: func(_ context.Context, opts *api.SyncCommitteeContributionOpts) (*api.Response[*altair.SyncCommitteeContribution], error) {
			// The contribution must be requested for the head root resolved above.
			require.Equal(t, headRoot, opts.BeaconBlockRoot)
			return &api.Response[*altair.SyncCommitteeContribution]{
				Data: &altair.SyncCommitteeContribution{
					Slot:              opts.Slot,
					BeaconBlockRoot:   opts.BeaconBlockRoot,
					SubcommitteeIndex: opts.SubcommitteeIndex,
				},
			}, nil
		},
	}

	goClient := &GoClient{
		log:          zap.NewNop(),
		beaconConfig: &cfg,
		multiClient:  client,
	}

	got, version, err := goClient.GetSyncCommitteeContribution(t.Context(), slot, selectionProofs, subnetIDs)
	require.NoError(t, err)
	require.Equal(t, spec.DataVersionAltair, version)

	contributions, ok := got.(*spectypes.Contributions)
	require.True(t, ok)
	require.Len(t, *contributions, len(subnetIDs))
	require.Equal(t, headRoot, (*contributions)[0].Contribution.BeaconBlockRoot)
}

// TestGetSyncCommitteeContributionPropagatesCanceledContext locks in the cancellation
// path of the 1/3-into-slot wait. With a future slot (so the wait blocks instead of
// early-returning) and an already-canceled context, GetSyncCommitteeContribution must
// return the wrapped "wait for 1/3 of slot" error and DataVersionNil, without ever
// querying the beacon node.
func TestGetSyncCommitteeContributionPropagatesCanceledContext(t *testing.T) {
	t.Parallel()

	cfg := *networkconfig.TestNetwork.Beacon
	// A slot well into the future so waitIntoSlot blocks rather than early-returning.
	slot := cfg.EstimatedCurrentSlot() + 1_000_000
	selectionProofs := []phase0.BLSSignature{signatureWithFirstByte(1)}
	subnetIDs := []uint64{7}

	client := &syncCommitteeClientMock{
		beaconBlockRootFunc: func(_ context.Context, _ *api.BeaconBlockRootOpts) (*api.Response[*phase0.Root], error) {
			t.Error("beacon node must not be queried when the context is canceled before the wait completes")
			return nil, errors.New("unexpected call")
		},
	}

	goClient := &GoClient{
		log:          zap.NewNop(),
		beaconConfig: &cfg,
		multiClient:  client,
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, version, err := goClient.GetSyncCommitteeContribution(ctx, slot, selectionProofs, subnetIDs)
	require.ErrorContains(t, err, "wait for 1/3 of slot")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, DataVersionNil, version)
	require.Nil(t, got)
}

func TestIsSyncCommitteeAggregatorHandlesZeroModulo(t *testing.T) {
	t.Parallel()

	goClient := &GoClient{
		beaconConfig: &networkconfig.Beacon{
			SyncCommitteeSize:                    1,
			SyncCommitteeSubnetCount:             4,
			TargetAggregatorsPerSyncSubcommittee: 1,
		},
	}

	require.True(t, goClient.IsSyncCommitteeAggregator(make([]byte, 96)))
}

func signatureWithFirstByte(first byte) phase0.BLSSignature {
	return phase0.BLSSignature{first}
}
