package utils

import (
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/networkconfig"
)

type SlotValue struct {
	mu   sync.Mutex
	slot phase0.Slot
}

func (sv *SlotValue) SetSlot(s phase0.Slot) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.slot = s
}

func (sv *SlotValue) GetSlot() phase0.Slot {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.slot
}

func SetupMockNetworkConfig(t *testing.T, currentSlot *SlotValue) *networkconfig.MockInterface {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	if currentSlot == nil {
		currentSlot = &SlotValue{}
		currentSlot.SetSlot(32)
	}

	mockBeaconNetwork := networkconfig.NewMockInterface(ctrl)

	mockBeaconNetwork.EXPECT().BeaconNetwork().DoAndReturn(
		func() string {
			return networkconfig.HoleskyBeaconConfig.ConfigName
		},
	).AnyTimes()

	mockBeaconNetwork.EXPECT().EstimatedCurrentEpoch().DoAndReturn(
		func() phase0.Epoch {
			return phase0.Epoch(currentSlot.GetSlot() / 32)
		},
	).AnyTimes()

	mockBeaconNetwork.EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot.GetSlot()
		},
	).AnyTimes()

	mockBeaconNetwork.EXPECT().EstimatedEpochAtSlot(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) phase0.Epoch {
			return phase0.Epoch(slot / 32)
		},
	).AnyTimes()

	mockBeaconNetwork.EXPECT().SlotDuration().DoAndReturn(
		func() time.Duration {
			return 12 * time.Second
		},
	).AnyTimes()

	mockBeaconNetwork.EXPECT().SlotsPerEpoch().DoAndReturn(
		func() uint64 {
			return 32
		},
	).AnyTimes()

	mockBeaconNetwork.EXPECT().DomainType().DoAndReturn(
		func() spectypes.DomainType {
			return networkconfig.TestingNetworkConfig.DomainType()
		},
	).AnyTimes()

	return mockBeaconNetwork
}
