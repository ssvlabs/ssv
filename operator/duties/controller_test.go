package duties

import (
	"context"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"
)

func TestDutyController_ListenToTicker(t *testing.T) {
	f := fetcherMock{}
	var wg sync.WaitGroup
	ctrl := &dutyController{
		logger: zap.L(), ctx: context.Background(), ethNetwork: beacon.NewNetwork(core.PraterNetwork),
		executor: execWithWaitGroup(t, &wg), fetcher: &f, genesisEpoch: 0, dutyLimit: 32,
	}
	cn := make(chan types.Slot)

	secPerSlot = 2
	defer func() {
		secPerSlot = 12
	}()
	currentSlot := ctrl.ethNetwork.EstimatedCurrentSlot()
	duties := map[types.Slot][]beacon.Duty{}
	duties[currentSlot] = []beacon.Duty{
		{Slot: spec.Slot(currentSlot), PubKey: spec.BLSPubKey{}},
	}
	duties[currentSlot+1] = []beacon.Duty{
		{Slot: spec.Slot(currentSlot + 1), PubKey: spec.BLSPubKey{}},
	}
	f.results = duties
	go ctrl.listenToTicker(cn)
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

	require.True(t, ctrl.shouldExecute(&beacon.Duty{Slot: spec.Slot(currentSlot), PubKey: spec.BLSPubKey{}}))
	require.False(t, ctrl.shouldExecute(&beacon.Duty{Slot: spec.Slot(currentSlot - 1000), PubKey: spec.BLSPubKey{}}))
	require.False(t, ctrl.shouldExecute(&beacon.Duty{Slot: spec.Slot(currentSlot + 1000), PubKey: spec.BLSPubKey{}}))
}

func TestDutyController_GetSlotStartTime(t *testing.T) {
	d := dutyController{logger: zap.L(), ethNetwork: beacon.NewNetwork(core.PraterNetwork)}

	ts := d.ethNetwork.GetSlotStartTime(646523)
	require.Equal(t, int64(1624266276), ts.Unix())
}

func TestDutyController_GetCurrentSlot(t *testing.T) {
	d := dutyController{logger: zap.L(), ethNetwork: beacon.NewNetwork(core.PraterNetwork)}

	slot := d.ethNetwork.EstimatedCurrentSlot()
	require.Greater(t, slot, int64(646855))
}

func TestDutyController_GetEpochFirstSlot(t *testing.T) {
	d := dutyController{logger: zap.L(), ethNetwork: beacon.NewNetwork(core.PraterNetwork)}

	slot := d.getEpochFirstSlot(20203)
	require.Equal(t, uint64(646496), slot)
}

type executorMock struct {
	t  *testing.T
	wg *sync.WaitGroup
}

func (e *executorMock) ExecuteDuty(duty *beacon.Duty) error {
	require.NotNil(e.t, duty)
	require.True(e.t, duty.Slot > 0)
	e.wg.Done()
	return nil
}

func execWithWaitGroup(t *testing.T, wg *sync.WaitGroup) DutyExecutor {
	return &executorMock{t, wg}
}

type fetcherMock struct {
	results map[types.Slot][]beacon.Duty
}

func (f *fetcherMock) GetDuties(slot uint64) ([]beacon.Duty, error) {
	return f.results[types.Slot(slot)], nil
}
