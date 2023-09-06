package dutyfetcher

import (
	"context"
	"fmt"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/slot_ticker"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func TestFetcher_Start(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	networkCfg := networkconfig.TestNetwork

	beaconNode := beacon.NewMockBeaconNode(ctrl)
	beaconNode.EXPECT().GetBeaconNetwork().Return(networkconfig.TestNetwork.Beacon.GetNetwork().BeaconNetwork).AnyTimes()
	beaconNode.EXPECT().ProposerDuties(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*eth2apiv1.ProposerDuty{{ValidatorIndex: 11}}, nil).AnyTimes()
	beaconNode.EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*eth2apiv1.SyncCommitteeDuty{{ValidatorIndex: 12}}, nil).AnyTimes()

	slotTicker := slot_ticker.NewTicker(ctx, networkCfg)
	vm := validatorsmap.New(ctx, validatorsmap.WithInitialState(map[string]*validator.Validator{
		"1": {
			Share: &ssvtypes.SSVShare{
				Metadata: ssvtypes.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Status:          eth2apiv1.ValidatorStateActiveOngoing,
						Index:           11,
						ActivationEpoch: 0,
					},
				},
			},
		},
		"2": {
			Share: &ssvtypes.SSVShare{
				Metadata: ssvtypes.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Status:          eth2apiv1.ValidatorStateActiveOngoing,
						Index:           12,
						ActivationEpoch: 0,
					},
				},
			},
		},
	}))

	f := New(beaconNode, slotTicker, vm)
	f.ticker <- 21

	go f.Start(ctx)

	time.Sleep(1 * time.Millisecond)

	require.NotNil(t, f.ProposerDuty(21, 11))
	require.Nil(t, f.ProposerDuty(21, 12))

	require.NotNil(t, f.SyncCommitteeDuty(21, 12))
	require.Nil(t, f.SyncCommitteeDuty(21, 11))
}

func TestFetcher_ProposerDuty(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	networkCfg := networkconfig.TestNetwork

	beaconNode := beacon.NewMockBeaconNode(ctrl)
	beaconNode.EXPECT().GetBeaconNetwork().Return(networkconfig.TestNetwork.Beacon.GetNetwork().BeaconNetwork).AnyTimes()
	beaconNode.EXPECT().ProposerDuties(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*eth2apiv1.ProposerDuty{{ValidatorIndex: 11}}, nil).AnyTimes()
	beaconNode.EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*eth2apiv1.SyncCommitteeDuty{{ValidatorIndex: 12}}, nil).AnyTimes()

	slotTicker := slot_ticker.NewTicker(ctx, networkCfg)

	vm := validatorsmap.New(ctx, validatorsmap.WithInitialState(map[string]*validator.Validator{
		"1": {
			Share: &ssvtypes.SSVShare{
				Metadata: ssvtypes.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Status:          eth2apiv1.ValidatorStateActiveOngoing,
						Index:           11,
						ActivationEpoch: 0,
					},
				},
			},
		},
		"2": {
			Share: &ssvtypes.SSVShare{
				Metadata: ssvtypes.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Status:          eth2apiv1.ValidatorStateActiveOngoing,
						Index:           12,
						ActivationEpoch: 0,
					},
				},
			},
		},
	}))

	f := New(beaconNode, slotTicker, vm)
	go f.Start(ctx)

	proposerDuties := map[phase0.ValidatorIndex]*eth2apiv1.ProposerDuty{
		11: {},
	}

	syncCommitteeDuties := map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty{
		12: {},
	}

	f.proposer.Set(f.beaconNode.GetBeaconNetwork().EstimatedEpochAtSlot(1), proposerDuties, 60*time.Second)
	f.syncCommittee.Set(f.beaconNode.GetBeaconNetwork().EstimatedEpochAtSlot(1), syncCommitteeDuties, 60*time.Second)

	require.NotNil(t, f.ProposerDuty(21, 11))
	require.NotNil(t, f.SyncCommitteeDuty(21, 12))

	f.Stop()
}

func TestFetcher_Start_FetchEpoch_Error(t *testing.T) {
	ctrl := gomock.NewController(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	networkCfg := networkconfig.TestNetwork

	expectedError := fmt.Errorf("fetch error")

	beaconNode := beacon.NewMockBeaconNode(ctrl)
	beaconNode.EXPECT().GetBeaconNetwork().Return(networkconfig.TestNetwork.Beacon.GetNetwork().BeaconNetwork).AnyTimes()
	beaconNode.EXPECT().ProposerDuties(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, expectedError).AnyTimes()
	beaconNode.EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, expectedError).AnyTimes()

	slotTicker := slot_ticker.NewTicker(ctx, networkCfg)

	vm := validatorsmap.New(ctx, validatorsmap.WithInitialState(map[string]*validator.Validator{
		"1": {
			Share: &ssvtypes.SSVShare{
				Metadata: ssvtypes.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Status:          eth2apiv1.ValidatorStateActiveOngoing,
						Index:           11,
						ActivationEpoch: 0,
					},
				},
			},
		},
		"2": {
			Share: &ssvtypes.SSVShare{
				Metadata: ssvtypes.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Status:          eth2apiv1.ValidatorStateActiveOngoing,
						Index:           12,
						ActivationEpoch: 0,
					},
				},
			},
		},
	}))

	f := New(beaconNode, slotTicker, vm)
	go f.Start(ctx)

	go f.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	f.ticker <- 21
	time.Sleep(100 * time.Millisecond)

	require.Nil(t, f.ProposerDuty(21, 11))
	require.Nil(t, f.SyncCommitteeDuty(21, 11))
}
