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

	exampleConfig := networkconfig.TestingBeaconConfig
	exampleConfig.NetworkName = string(spectypes.HoleskyNetwork)

	mockNetworkConfig := networkconfig.NewMockInterface(ctrl)
	mockNetworkConfig.EXPECT().BeaconNetwork().DoAndReturn(
		func() string {
			return exampleConfig.NetworkName
		},
	).AnyTimes()
	mockNetworkConfig.EXPECT().SlotsPerEpoch().Return(exampleConfig.SlotsPerEpoch).AnyTimes()
	mockNetworkConfig.EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot.GetSlot()
		},
	).AnyTimes()
	mockNetworkConfig.EXPECT().EstimatedCurrentEpoch().DoAndReturn(
		func() phase0.Epoch {
			return phase0.Epoch(currentSlot.GetSlot() / mockNetworkConfig.SlotsPerEpoch())
		},
	).AnyTimes()
	mockNetworkConfig.EXPECT().EstimatedEpochAtSlot(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) phase0.Epoch {
			return exampleConfig.EstimatedEpochAtSlot(slot)
		},
	).AnyTimes()
	mockNetworkConfig.EXPECT().EstimatedSlotAtTime(gomock.Any()).DoAndReturn(
		func(v time.Time) phase0.Slot {
			return exampleConfig.EstimatedSlotAtTime(v)
		},
	).AnyTimes()
	mockNetworkConfig.EXPECT().FirstSlotAtEpoch(gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch) phase0.Slot {
			return exampleConfig.FirstSlotAtEpoch(epoch)
		},
	).AnyTimes()
	mockNetworkConfig.EXPECT().IsFirstSlotOfEpoch(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) bool {
			return (slot)%mockNetworkConfig.SlotsPerEpoch() == 0
		},
	).AnyTimes()
	mockNetworkConfig.EXPECT().GetSlotStartTime(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) time.Time {
			return exampleConfig.GetSlotStartTime(slot)
		},
	).AnyTimes()
	mockNetworkConfig.EXPECT().SlotDuration().DoAndReturn(
		func() time.Duration {
			return exampleConfig.SlotDuration
		},
	).AnyTimes()
	mockNetworkConfig.EXPECT().DomainType().DoAndReturn(
		func() spectypes.DomainType {
			return networkconfig.TestingNetworkConfig.DomainType()
		},
	).AnyTimes()

	return mockNetworkConfig
}
