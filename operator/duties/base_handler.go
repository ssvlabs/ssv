package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/networkconfig"
)

//go:generate mockgen -package=duties -destination=./base_handler_mock.go -source=./base_handler.go

// ExecuteDutiesFunc is a non-blocking functions which executes the given duties.
type ExecuteDutiesFunc func(logger *zap.Logger, duties []*spectypes.Duty)

type dutyHandler interface {
	Setup(string, *zap.Logger, BeaconNode, networkconfig.NetworkConfig, ValidatorController, ExecuteDutiesFunc, chan phase0.Slot, chan ReorgEvent, chan struct{})
	HandleDuties(context.Context)
	Name() string
}

type baseHandler struct {
	logger              *zap.Logger
	beaconNode          BeaconNode
	network             networkconfig.NetworkConfig
	validatorController ValidatorController
	executeDuties       ExecuteDutiesFunc
	ticker              chan phase0.Slot

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
	ticker chan phase0.Slot,
	reorgEvents chan ReorgEvent,
	indicesChange chan struct{},
) {
	h.logger = logger.With(zap.String("handler", name))
	h.beaconNode = beaconNode
	h.network = network
	h.validatorController = validatorController
	h.executeDuties = executeDuties
	h.ticker = ticker
	h.reorg = reorgEvents
	h.indicesChange = indicesChange
}
