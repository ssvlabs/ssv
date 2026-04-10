package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=duties -destination=./base_handler_mock.go -source=./base_handler.go

type dutyHandler interface {
	Setup(
		ctx context.Context,
		name string,
		logger *zap.Logger,
		beaconNode BeaconNode,
		executionClient ExecutionClient,
		beaconConfig *networkconfig.Beacon,
		validatorProvider ValidatorProvider,
		validatorController ValidatorController,
		dutiesExecutor DutiesExecutor,
		slotTickerProvider slotticker.Provider,
		reorgEventsCh chan ReorgEvent,
		indicesChangeCh chan struct{},
	)
	HandleDuties(context.Context)
	HandleInitialDuties(context.Context)
	Name() string
	WaitShutdown()
}

type baseHandler struct {
	logger *zap.Logger

	// ctx controls the lifetime of all go-routines spawned by baseHandler.
	ctx context.Context

	beaconNode          BeaconNode
	executionClient     ExecutionClient
	beaconConfig        *networkconfig.Beacon
	validatorProvider   ValidatorProvider
	validatorController ValidatorController
	dutiesExecutor      DutiesExecutor
	ticker              slotticker.SlotTicker

	reorgCh         chan ReorgEvent
	indicesChangeCh chan struct{}

	indicesChanged bool
}

func (h *baseHandler) Setup(
	ctx context.Context,
	name string,
	logger *zap.Logger,
	beaconNode BeaconNode,
	executionClient ExecutionClient,
	beaconConfig *networkconfig.Beacon,
	validatorProvider ValidatorProvider,
	validatorController ValidatorController,
	dutiesExecutor DutiesExecutor,
	slotTickerProvider slotticker.Provider,
	reorgEventsCh chan ReorgEvent,
	indicesChangeCh chan struct{},
) {
	h.logger = logger.With(zap.String("handler", name))
	h.ctx = ctx
	h.beaconNode = beaconNode
	h.executionClient = executionClient
	h.beaconConfig = beaconConfig
	h.validatorProvider = validatorProvider
	h.validatorController = validatorController
	h.dutiesExecutor = dutiesExecutor
	h.ticker = slotTickerProvider()
	h.reorgCh = reorgEventsCh
	h.indicesChangeCh = indicesChangeCh
}

func (h *baseHandler) warnMisalignedSlotAndDuty(dutyType string) {
	h.logger.Debug("current slot and duty slot are not aligned, "+
		"assuming diff caused by a time drift - ignoring and executing duty", zap.String("type", dutyType))
}

func (h *baseHandler) HandleInitialDuties(context.Context) {
	// Do nothing
}

// shouldFetchNextEpoch returns true if it is a "good time" to fetch duties for the next epoch (typically, Beacon node
// would be under less load during the mid-end time into the epoch vs during the beginning of the epoch).
func (h *baseHandler) shouldFetchNextEpoch(currentSlot phase0.Slot) bool {
	slotsPerEpoch := h.beaconConfig.SlotsPerEpoch
	return uint64(currentSlot)%slotsPerEpoch > slotsPerEpoch/2-2
}

func (h *baseHandler) atLastSlotOfCurrentEpoch(currentSlot phase0.Slot) bool {
	slotsPerEpoch := h.beaconConfig.SlotsPerEpoch
	return uint64(currentSlot+1)%slotsPerEpoch == 0
}
