package goclient

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
)

type testProposal struct {
	id           uint64
	feeRecipient bool
	delay        time.Duration
}

func TestGoClient_selectBestProposal(t *testing.T) {
	tt := []struct {
		name           string
		ctxTimeout     time.Duration
		proposals      []testProposal
		extraDelay     time.Duration
		wantProposalID uint64
		wantDelay      time.Duration
		wantErr        string
	}{
		{
			name:       "got both immediately",
			ctxTimeout: 2 * time.Second,
			proposals: []testProposal{
				{
					id:           1,
					feeRecipient: true,
					delay:        0,
				},
				{
					id:           2,
					feeRecipient: false,
					delay:        0,
				},
			},
			wantProposalID: 1,
			wantDelay:      0,
			wantErr:        "",
		},
		{
			name:       "got fee recipient later",
			ctxTimeout: 2 * time.Second,
			proposals: []testProposal{
				{
					id:           1,
					feeRecipient: true,
					delay:        100 * time.Millisecond,
				},
				{
					id:           2,
					feeRecipient: false,
					delay:        0,
				},
			},
			wantProposalID: 1,
			wantDelay:      100 * time.Millisecond,
			wantErr:        "",
		},
		{
			name:       "got proposal later",
			ctxTimeout: 2 * time.Second,
			proposals: []testProposal{
				{
					id:           1,
					feeRecipient: true,
					delay:        0,
				},
				{
					id:           2,
					feeRecipient: false,
					delay:        100 * time.Millisecond,
				},
			},
			wantProposalID: 1,
			wantDelay:      0,
			wantErr:        "",
		},
		{
			name:       "got fee recipient too late",
			ctxTimeout: 2 * time.Second,
			proposals: []testProposal{
				{
					id:           1,
					feeRecipient: true,
					delay:        1500 * time.Millisecond,
				},
				{
					id:           2,
					feeRecipient: false,
					delay:        1300 * time.Millisecond,
				},
			},
			wantProposalID: 2,
			wantDelay:      1300 * time.Millisecond,
			wantErr:        "",
		},
		{
			name:           "immediate errors",
			ctxTimeout:     2 * time.Second,
			proposals:      []testProposal{},
			wantProposalID: 0,
			wantDelay:      0,
			wantErr:        "all requests failed",
		},
		{
			name:           "late errors",
			ctxTimeout:     2 * time.Second,
			extraDelay:     1400 * time.Millisecond,
			proposals:      []testProposal{},
			wantProposalID: 0,
			wantDelay:      1400 * time.Millisecond,
			wantErr:        "all requests failed",
		},
		{
			name:       "parent ctx timeout, no proposals at all",
			ctxTimeout: 100 * time.Millisecond,
			proposals: []testProposal{
				{
					id:           1,
					feeRecipient: true,
					delay:        200 * time.Millisecond,
				},
				{
					id:           2,
					feeRecipient: false,
					delay:        200 * time.Millisecond,
				},
			},
			wantProposalID: 1,
			wantDelay:      100 * time.Millisecond,
			wantErr:        "context deadline exceeded",
		},
		{
			name:       "parent ctx timeout, no fee recipients, but has proposal",
			ctxTimeout: 100 * time.Millisecond,
			proposals: []testProposal{
				{
					id:           1,
					feeRecipient: true,
					delay:        200 * time.Millisecond,
				},
				{
					id:           2,
					feeRecipient: false,
					delay:        0,
				},
			},
			wantProposalID: 2,
			wantDelay:      100 * time.Millisecond,
			wantErr:        "",
		},
		{
			name:       "never got fee recipient",
			ctxTimeout: 2 * time.Second,
			proposals: []testProposal{
				{
					id:           2,
					feeRecipient: false,
					delay:        0,
				},
			},
			wantProposalID: 2,
			wantDelay:      0,
			wantErr:        "",
		},
		{
			name:       "never got proposal",
			ctxTimeout: 2 * time.Second,
			proposals: []testProposal{
				{
					id:           1,
					feeRecipient: true,
					delay:        0,
				},
			},
			wantProposalID: 1,
			wantDelay:      0,
			wantErr:        "",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			beaconCfg := *networkconfig.TestNetwork.BeaconConfig
			beaconCfg.GenesisTime = time.Now()

			gc := &GoClient{
				log:          logging.TestLogger(t),
				beaconConfig: &beaconCfg,
			}

			proposals := make(chan *api.VersionedProposal)

			var wg sync.WaitGroup
			for _, tp := range tc.proposals {
				wg.Add(1)
				go func(p testProposal) {
					defer wg.Done()
					time.Sleep(p.delay)

					vp := &api.VersionedProposal{
						Version:        spec.DataVersionElectra,
						Blinded:        true,
						ConsensusValue: new(big.Int).SetUint64(p.id),
					}

					if p.feeRecipient {
						vp.ElectraBlinded = &apiv1electra.BlindedBeaconBlock{
							Body: &apiv1electra.BlindedBeaconBlockBody{
								ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
									FeeRecipient: bellatrix.ExecutionAddress{1, 2, 3, 4},
								},
							},
						}

						hasFR, err := hasFeeRecipient(vp)
						require.NoError(t, err)
						require.True(t, hasFR)
					}

					proposals <- vp
				}(tp)
			}
			go func() {
				wg.Wait()
				time.Sleep(tc.extraDelay)
				close(proposals)
			}()

			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
			defer cancel()

			proposal, err := gc.selectBestProposal(
				ctx,
				beaconCfg.EstimatedCurrentSlot(),
				proposals,
			)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, tc.wantProposalID, proposal.ConsensusValue.Uint64())
			}
			require.GreaterOrEqual(t, time.Since(beaconCfg.GenesisTime), tc.wantDelay)
		})
	}
}
