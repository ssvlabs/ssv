package utils

import (
	"math"
	"sync"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/mock/gomock"

	mocknetwork "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/mocks"
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

func SetupMockBeaconNetwork(t *testing.T, currentSlot *SlotValue) *mocknetwork.MockBeaconNetwork {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	if currentSlot == nil {
		currentSlot = &SlotValue{}
		currentSlot.SetSlot(32)
	}

	beaconNetwork := spectypes.HoleskyNetwork // it must be something known by ekm

	mockBeaconNetwork := mocknetwork.NewMockBeaconNetwork(ctrl)
	mockBeaconNetwork.EXPECT().GetBeaconNetwork().Return(beaconNetwork).AnyTimes()

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

	mockBeaconNetwork.EXPECT().GetEpochLastSlot(gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch) phase0.Slot {
			if uint64(epoch) == math.MaxUint64 {
				return phase0.Slot(math.MaxUint64)
			}
			return phase0.Slot(uint64(epoch+1)*32 - 1)
		},
	).AnyTimes()

	return mockBeaconNetwork
}
