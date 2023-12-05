package duties

import (
	"context"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/slotticker"
)

//go:generate mockgen -package=duties -destination=./base_handler_mock.go -source=./base_handler.go

// ExecuteDutiesFunc is a non-blocking functions which executes the given duties.
type ExecuteDutiesFunc func(logger *zap.Logger, duties []*spectypes.Duty)

type dutyHandler interface {
	Setup(string, *zap.Logger, BeaconNode, networkconfig.NetworkConfig, ValidatorController, ExecuteDutiesFunc, slotticker.Provider, chan ReorgEvent, chan struct{})
	HandleDuties(context.Context)
	HandleInitialDuties(context.Context)
	Name() string
}

type baseHandler struct {
	logger              *zap.Logger
	beaconNode          BeaconNode
	network             networkconfig.NetworkConfig
	validatorController ValidatorController
	executeDuties       ExecuteDutiesFunc
	ticker              slotticker.SlotTicker

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
	slotTickerProvider slotticker.Provider,
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

func (h *baseHandler) warnMisalignedSlotAndDuty(dutyType string) {
	h.logger.Debug("current slot and duty slot are not aligned, "+
		"assuming diff caused by a time drift - ignoring and executing duty", zap.String("type", dutyType))
}

func (b *baseHandler) HandleInitialDuties(context.Context) {
	// Do nothing
}
