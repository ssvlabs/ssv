package goclient

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
)

func TestGoClient_selectBestProposal(t *testing.T) {
	tt := []struct {
		name         string
		ctxTimeout   time.Duration
		frProposals  []uint64 // IDs; must be unique
		frDelay      time.Duration
		proposals    []uint64 // IDs; must be unique
		pDelay       time.Duration
		wantProposal uint64 // ID
		wantDelay    time.Duration
		wantErr      string
	}{
		{
			name:         "got both immediately",
			ctxTimeout:   2 * time.Second,
			frProposals:  []uint64{1},
			frDelay:      0,
			proposals:    []uint64{2},
			pDelay:       0,
			wantProposal: 1,
			wantDelay:    0,
			wantErr:      "",
		},
		{
			name:         "got fee recipient later",
			ctxTimeout:   2 * time.Second,
			frProposals:  []uint64{1},
			frDelay:      100 * time.Millisecond,
			proposals:    []uint64{2},
			pDelay:       0,
			wantProposal: 1,
			wantDelay:    100 * time.Millisecond,
			wantErr:      "",
		},
		{
			name:         "got proposal later",
			ctxTimeout:   2 * time.Second,
			frProposals:  []uint64{1},
			frDelay:      0,
			proposals:    []uint64{2},
			pDelay:       100 * time.Millisecond,
			wantProposal: 1,
			wantDelay:    0,
			wantErr:      "",
		},
		{
			name:         "got fee recipient too late",
			ctxTimeout:   2 * time.Second,
			frProposals:  []uint64{1},
			frDelay:      1500 * time.Millisecond,
			proposals:    []uint64{2},
			pDelay:       0,
			wantProposal: 2,
			wantDelay:    1300 * time.Millisecond,
			wantErr:      "",
		},
		{
			name:         "immediate errors",
			ctxTimeout:   2 * time.Second,
			frProposals:  []uint64{},
			frDelay:      0,
			proposals:    []uint64{},
			pDelay:       0,
			wantProposal: 0,
			wantDelay:    0,
			wantErr:      "all requests failed",
		},
		{
			name:         "late errors, proposal first",
			ctxTimeout:   2 * time.Second,
			frProposals:  []uint64{},
			frDelay:      1500 * time.Millisecond,
			proposals:    []uint64{},
			pDelay:       1400 * time.Millisecond,
			wantProposal: 0,
			wantDelay:    1400 * time.Millisecond,
			wantErr:      "all requests failed",
		},
		{
			name:         "late errors, fee recipient first",
			ctxTimeout:   2 * time.Second,
			frProposals:  []uint64{},
			frDelay:      1400 * time.Millisecond,
			proposals:    []uint64{},
			pDelay:       1500 * time.Millisecond,
			wantProposal: 0,
			wantDelay:    1500 * time.Millisecond,
			wantErr:      "all requests failed",
		},
		{
			name:         "parent ctx timeout, no proposals at all",
			ctxTimeout:   100 * time.Millisecond,
			frProposals:  []uint64{1},
			frDelay:      200 * time.Millisecond,
			proposals:    []uint64{2},
			pDelay:       200 * time.Millisecond,
			wantProposal: 1,
			wantDelay:    100 * time.Millisecond,
			wantErr:      "context deadline exceeded",
		},
		{
			name:         "parent ctx timeout, no fee recipients, but has proposal",
			ctxTimeout:   100 * time.Millisecond,
			frProposals:  []uint64{1},
			frDelay:      200 * time.Millisecond,
			proposals:    []uint64{2},
			pDelay:       0 * time.Millisecond,
			wantProposal: 2,
			wantDelay:    100 * time.Millisecond,
			wantErr:      "",
		},
		{
			name:         "never got fee recipient",
			ctxTimeout:   2 * time.Second,
			frProposals:  []uint64{},
			frDelay:      0,
			proposals:    []uint64{2},
			pDelay:       0,
			wantProposal: 2,
			wantDelay:    0,
			wantErr:      "",
		},
		{
			name:         "never got proposal",
			ctxTimeout:   2 * time.Second,
			frProposals:  []uint64{1},
			frDelay:      0,
			proposals:    []uint64{},
			pDelay:       0,
			wantProposal: 1,
			wantDelay:    0,
			wantErr:      "",
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

			proposals := make(chan *api.VersionedProposal, len(tc.proposals))
			frProposals := make(chan *api.VersionedProposal, len(tc.frProposals))

			go func() {
				time.Sleep(tc.frDelay)
				for _, proposal := range tc.frProposals {
					frProposals <- &api.VersionedProposal{
						ConsensusValue: new(big.Int).SetUint64(proposal),
					}
				}
				close(frProposals)
			}()

			go func() {
				time.Sleep(tc.pDelay)
				for _, proposal := range tc.proposals {
					proposals <- &api.VersionedProposal{
						ConsensusValue: new(big.Int).SetUint64(proposal),
					}
				}
				close(proposals)
			}()

			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
			defer cancel()

			proposal, err := gc.selectBestProposal(
				ctx,
				beaconCfg.EstimatedCurrentSlot(),
				frProposals,
				proposals,
			)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, tc.wantProposal, proposal.ConsensusValue.Uint64())
			}
			require.GreaterOrEqual(t, time.Since(beaconCfg.GenesisTime), tc.wantDelay)
		})
	}
}
