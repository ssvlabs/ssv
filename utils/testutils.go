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

func SetupMockNetworkConfig(t *testing.T, domainType spectypes.DomainType, currentSlot *SlotValue) *networkconfig.MockNetwork {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	if currentSlot == nil {
		currentSlot = &SlotValue{}
		currentSlot.SetSlot(32)
	}

	beaconNetwork := spectypes.HoleskyNetwork // it must be something known by ekm
	mockNetwork := networkconfig.NewMockNetwork(ctrl)

	mockNetwork.EXPECT().GetSlotsPerEpoch().Return(beaconNetwork.SlotsPerEpoch()).AnyTimes()
	mockNetwork.EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot.GetSlot()
		},
	).AnyTimes()
	mockNetwork.EXPECT().EstimatedCurrentEpoch().DoAndReturn(
		func() phase0.Epoch {
			return phase0.Epoch(uint64(currentSlot.GetSlot()) / beaconNetwork.SlotsPerEpoch())
		},
	).AnyTimes()
	mockNetwork.EXPECT().EstimatedEpochAtSlot(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) phase0.Epoch {
			return beaconNetwork.EstimatedEpochAtSlot(slot)
		},
	).AnyTimes()
	mockNetwork.EXPECT().FirstSlotAtEpoch(gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch) phase0.Slot {
			return beaconNetwork.FirstSlotAtEpoch(epoch)
		},
	).AnyTimes()
	mockNetwork.EXPECT().IsFirstSlotOfEpoch(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) bool {
			return uint64(slot)%mockNetwork.GetSlotsPerEpoch() == 0
		},
	).AnyTimes()
	mockNetwork.EXPECT().GetSlotStartTime(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) time.Time {
			timeSinceGenesisStart := time.Duration(slot) & beaconNetwork.SlotDurationSec() // #nosec G115
			minGenesisTime := mockNetwork.GetGenesisTime()                                 // #nosec G115
			start := minGenesisTime.Add(timeSinceGenesisStart)
			return start
		},
	).AnyTimes()
	mockNetwork.EXPECT().EstimatedSlotAtTime(gomock.Any()).DoAndReturn(func(timeVal time.Time) phase0.Slot {
		return beaconNetwork.EstimatedSlotAtTime(timeVal.Unix())
	}).AnyTimes()

	mockNetwork.EXPECT().GetSlotDuration().DoAndReturn(
		func() time.Duration {
			return 12 * time.Second
		},
	).AnyTimes()

	mockNetwork.EXPECT().GetSlotsPerEpoch().DoAndReturn(
		func() phase0.Slot {
			return 32
		},
	).AnyTimes()

	mockNetwork.EXPECT().GetGenesisTime().DoAndReturn(
		func() time.Time {
			return time.Unix(int64(beaconNetwork.MinGenesisTime()), 0) // #nosec G115 -- genesis time is never above int64
		},
	).AnyTimes()

	mockNetwork.EXPECT().EpochDuration().Return(time.Duration(mockNetwork.GetSlotsPerEpoch()) * mockNetwork.GetSlotDuration()).AnyTimes() // #nosec G115

	mockNetwork.EXPECT().IntervalDuration().Return(mockNetwork.GetSlotDuration() / 3).AnyTimes() // #nosec G115

	mockNetwork.EXPECT().GetDomainType().Return(domainType).AnyTimes()

	mockNetwork.EXPECT().GetNetworkName().Return(string(beaconNetwork)).AnyTimes()

	return mockNetwork
}
