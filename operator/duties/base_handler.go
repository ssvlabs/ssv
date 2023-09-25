package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/slot_ticker"
)

//go:generate mockgen -package=duties -destination=./base_handler_mock.go -source=./base_handler.go

// ExecuteDutiesFunc is a non-blocking functions which executes the given duties.
type ExecuteDutiesFunc func(logger *zap.Logger, duties []*spectypes.Duty)

type dutyHandler interface {
	Setup(string, *zap.Logger, BeaconNode, networkconfig.NetworkConfig, ValidatorController, ExecuteDutiesFunc, slot_ticker.SlotTickerProvider, chan ReorgEvent, chan struct{})
	HandleDuties(context.Context)
	Name() string
}

type baseHandler struct {
	logger              *zap.Logger
	beaconNode          BeaconNode
	network             networkconfig.NetworkConfig
	validatorController ValidatorController
	executeDuties       ExecuteDutiesFunc
	ticker              slot_ticker.SlotTicker

	reorg         chan ReorgEvent
	indicesChange chan struct{}

	fetchFirst     bool
	indicesChanged bool
}

func (h *baseHandler) Setup(
	name string,
	logger *zap.Logger,
	beaconNode BeaconNode,
	network networkconfig.NetworkConfig,
	validatorController ValidatorController,
	executeDuties ExecuteDutiesFunc,
	slotTickerProvider slot_ticker.SlotTickerProvider,
	reorgEvents chan ReorgEvent,
	indicesChange chan struct{},
) {
	h.logger = logger.With(zap.String("handler", name))
	h.beaconNode = beaconNode
	h.network = network
	h.validatorController = validatorController
	h.executeDuties = executeDuties
	h.ticker = slotTickerProvider()
	h.reorg = reorgEvents
	h.indicesChange = indicesChange
}

type Duties[D any] struct {
	m map[phase0.Epoch]map[phase0.Slot][]D
}

func NewDuties[D any]() *Duties[D] {
	return &Duties[D]{
		m: make(map[phase0.Epoch]map[phase0.Slot][]D),
	}
}

func (d *Duties[D]) Add(epoch phase0.Epoch, slot phase0.Slot, duty D) {
	if _, ok := d.m[epoch]; !ok {
		d.m[epoch] = make(map[phase0.Slot][]D)
	}
	d.m[epoch][slot] = append(d.m[epoch][slot], duty)
}

func (d *Duties[D]) Reset(epoch phase0.Epoch) {
	delete(d.m, epoch)
}
