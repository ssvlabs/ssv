package duties

import (
	"context"
	"sync"
	"testing"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/golang/mock/gomock"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/operator/duties/mocks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

func TestDutyController_ListenToTicker(t *testing.T) {
	var wg sync.WaitGroup

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockExecutor := mocks.NewMockDutyExecutor(mockCtrl)
	mockExecutor.EXPECT().ExecuteDuty(gomock.Any()).DoAndReturn(func(duty *spectypes.Duty) error {
		require.NotNil(t, duty)
		require.True(t, duty.Slot > 0)
		wg.Done()
		return nil
	}).AnyTimes()

	mockFetcher := mocks.NewMockDutyFetcher(mockCtrl)
	mockFetcher.EXPECT().GetDuties(gomock.Any()).DoAndReturn(func(slot uint64) ([]spectypes.Duty, error) {
		return []spectypes.Duty{{Slot: spec.Slot(slot), PubKey: spec.BLSPubKey{}}}, nil
	}).AnyTimes()

	dutyCtrl := &dutyController{
		logger: zap.L(), ctx: context.Background(), ethNetwork: beacon.NewNetwork(core.PraterNetwork),
		executor: mockExecutor,
		fetcher:  mockFetcher,
	}

	cn := make(chan types.Slot)

	secPerSlot = 2
	defer func() {
		secPerSlot = 12
	}()

	currentSlot := dutyCtrl.ethNetwork.EstimatedCurrentSlot()

	go dutyCtrl.listenToTicker(cn)
	wg.Add(2)
	go func() {
		cn <- currentSlot
		time.Sleep(time.Second * time.Duration(secPerSlot))
		cn <- currentSlot + 1
	}()

	wg.Wait()
}

func TestDutyController_ShouldExecute(t *testing.T) {
	ctrl := dutyController{logger: zap.L(), ethNetwork: beacon.NewNetwork(core.PraterNetwork)}
	currentSlot := uint64(ctrl.ethNetwork.EstimatedCurrentSlot())

	require.True(t, ctrl.shouldExecute(&spectypes.Duty{Slot: spec.Slot(currentSlot), PubKey: spec.BLSPubKey{}}))
	require.False(t, ctrl.shouldExecute(&spectypes.Duty{Slot: spec.Slot(currentSlot - 1000), PubKey: spec.BLSPubKey{}}))
	require.False(t, ctrl.shouldExecute(&spectypes.Duty{Slot: spec.Slot(currentSlot + 1000), PubKey: spec.BLSPubKey{}}))
}

func TestDutyController_GetSlotStartTime(t *testing.T) {
	d := dutyController{logger: zap.L(), ethNetwork: beacon.NewNetwork(core.PraterNetwork)}

	ts := d.ethNetwork.GetSlotStartTime(646523)
	require.Equal(t, int64(1624266276), ts.Unix())
}

func TestDutyController_GetCurrentSlot(t *testing.T) {
	d := dutyController{logger: zap.L(), ethNetwork: beacon.NewNetwork(core.PraterNetwork)}

	slot := d.ethNetwork.EstimatedCurrentSlot()
	require.Greater(t, slot, types.Slot(646855))
}

func TestDutyController_GetEpochFirstSlot(t *testing.T) {
	d := dutyController{logger: zap.L(), ethNetwork: beacon.NewNetwork(core.PraterNetwork)}

	slot := d.getEpochFirstSlot(20203)
	require.EqualValues(t, 646496, slot)
}
