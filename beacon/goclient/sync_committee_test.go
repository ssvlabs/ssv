package goclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
		mu        sync.Mutex
		requested []uint64
	)
	client := &syncCommitteeClientMock{
		beaconBlockRootFunc: func(_ context.Context, opts *api.BeaconBlockRootOpts) (*api.Response[*phase0.Root], error) {
			require.Equal(t, fmt.Sprint(slot), opts.Block)
			return &api.Response[*phase0.Root]{Data: &blockRoot}, nil
		},
		syncCommitteeContributionFunc: func(_ context.Context, opts *api.SyncCommitteeContributionOpts) (*api.Response[*altair.SyncCommitteeContribution], error) {
			mu.Lock()
			requested = append(requested, opts.SubcommitteeIndex)
			mu.Unlock()

			switch opts.SubcommitteeIndex {
			case subnetIDs[0]:
				time.Sleep(25 * time.Millisecond)
			case subnetIDs[1]:
				time.Sleep(5 * time.Millisecond)
			}

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
	require.ElementsMatch(t, subnetIDs, requested)

	for index, contribution := range *contributions {
		require.NotNil(t, contribution)
		require.Equal(t, [96]byte(selectionProofs[index]), contribution.SelectionProofSig)
		require.Equal(t, subnetIDs[index], contribution.Contribution.SubcommitteeIndex)
		require.Equal(t, blockRoot, contribution.Contribution.BeaconBlockRoot)
		require.Equal(t, slot, contribution.Contribution.Slot)
	}
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
	var sig phase0.BLSSignature
	sig[0] = first
	return sig
}
