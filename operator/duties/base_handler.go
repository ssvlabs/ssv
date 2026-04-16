package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=duties -destination=./base_handler_mock.go -source=./base_handler.go

type SetupOptions struct {
	Name                string
	Logger              *zap.Logger
	BeaconNode          BeaconNode
	ExecutionClient     ExecutionClient
	BeaconConfig        *networkconfig.Beacon
	ValidatorProvider   ValidatorProvider
	ValidatorController ValidatorController
	DutiesExecutor      DutiesExecutor
	SlotTickerProvider  slotticker.Provider
	ReorgEvents         chan ReorgEvent
	IndicesChange       chan struct{}
}

type dutyHandler interface {
	Setup(ctx context.Context, opts SetupOptions)
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

	reorg         chan ReorgEvent
	indicesChange chan struct{}
}

func (h *baseHandler) Setup(ctx context.Context, opts SetupOptions) {
	h.logger = opts.Logger.With(zap.String("handler", opts.Name))
	h.ctx = ctx
	h.beaconNode = opts.BeaconNode
	h.executionClient = opts.ExecutionClient
	h.beaconConfig = opts.BeaconConfig
	h.validatorProvider = opts.ValidatorProvider
	h.validatorController = opts.ValidatorController
	h.dutiesExecutor = opts.DutiesExecutor
	h.ticker = opts.SlotTickerProvider()
	h.reorg = opts.ReorgEvents
	h.indicesChange = opts.IndicesChange
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
