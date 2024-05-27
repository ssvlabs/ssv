package duties

import (
	"context"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
)

//go:generate mockgen -package=duties -destination=./base_handler_mock.go -source=./base_handler.go

// ExecuteDutiesFunc is a non-blocking functions which executes the given duties.
type ExecuteDutiesFunc func(logger *zap.Logger, duties []*spectypes.BeaconDuty)

// ExecuteCommitteeDutiesFunc is a non-blocking function which executes the given committee duties.
type ExecuteCommitteeDutiesFunc func(logger *zap.Logger, duties map[[32]byte]*spectypes.CommitteeDuty)

type dutyHandler interface {
	Setup(string, *zap.Logger, BeaconNode, ExecutionClient, networkconfig.NetworkConfig, ValidatorProvider, ExecuteDutiesFunc, ExecuteCommitteeDutiesFunc, slotticker.Provider, chan ReorgEvent, chan struct{})
	HandleDuties(context.Context)
	HandleInitialDuties(context.Context)
	Name() string
}

type baseHandler struct {
	logger                 *zap.Logger
	beaconNode             BeaconNode
	executionClient        ExecutionClient
	network                networkconfig.NetworkConfig
	validatorProvider      ValidatorProvider
	executeDuties          ExecuteDutiesFunc
	executeCommitteeDuties ExecuteCommitteeDutiesFunc
	ticker                 slotticker.SlotTicker

	reorg         chan ReorgEvent
	indicesChange chan struct{}

	fetchFirst     bool
	indicesChanged bool
}

func (h *baseHandler) Setup(
	name string,
	logger *zap.Logger,
	beaconNode BeaconNode,
	executionClient ExecutionClient,
	network networkconfig.NetworkConfig,
	validatorProvider ValidatorProvider,
	executeDuties ExecuteDutiesFunc,
	executeCommitteeDuties ExecuteCommitteeDutiesFunc,
	slotTickerProvider slotticker.Provider,
	reorgEvents chan ReorgEvent,
	indicesChange chan struct{},
) {
	h.logger = logger.With(zap.String("handler", name))
	h.beaconNode = beaconNode
	h.executionClient = executionClient
	h.network = network
	h.validatorProvider = validatorProvider
	h.executeDuties = executeDuties
	h.executeCommitteeDuties = executeCommitteeDuties
	h.ticker = slotTickerProvider()
	h.reorg = reorgEvents
	h.indicesChange = indicesChange
}

func (h *baseHandler) warnMisalignedSlotAndDuty(dutyType string) {
	h.logger.Debug("current slot and duty slot are not aligned, "+
		"assuming diff caused by a time drift - ignoring and executing duty", zap.String("type", dutyType))
}

func (h *baseHandler) HandleInitialDuties(context.Context) {
	// Do nothing
}
