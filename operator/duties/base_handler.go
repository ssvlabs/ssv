package duties

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=duties -destination=./base_handler_mock.go -source=./base_handler.go

type dutyHandler interface {
	Setup(
		name string,
		logger *zap.Logger,
		beaconNode BeaconNode,
		executionClient ExecutionClient,
		beaconConfig *networkconfig.Beacon,
		validatorProvider ValidatorProvider,
		validatorController ValidatorController,
		dutiesExecutor DutiesExecutor,
		slotTickerProvider slotticker.Provider,
		reorgEvents chan ReorgEvent,
		indicesChange chan struct{},
	)
	HandleDuties(context.Context)
	HandleInitialDuties(context.Context)
	Name() string
}

type baseHandler struct {
	logger              *zap.Logger
	beaconNode          BeaconNode
	executionClient     ExecutionClient
	beaconConfig        *networkconfig.Beacon
	validatorProvider   ValidatorProvider
	validatorController ValidatorController
	dutiesExecutor      DutiesExecutor
	ticker              slotticker.SlotTicker

	reorg         chan ReorgEvent
	indicesChange chan struct{}

	indicesChanged bool
}

func (h *baseHandler) Setup(
	name string,
	logger *zap.Logger,
	beaconNode BeaconNode,
	executionClient ExecutionClient,
	beaconConfig *networkconfig.Beacon,
	validatorProvider ValidatorProvider,
	validatorController ValidatorController,
	dutiesExecutor DutiesExecutor,
	slotTickerProvider slotticker.Provider,
	reorgEvents chan ReorgEvent,
	indicesChange chan struct{},
) {
	h.logger = logger.With(zap.String("handler", name))
	h.beaconNode = beaconNode
	h.executionClient = executionClient
	h.beaconConfig = beaconConfig
	h.validatorProvider = validatorProvider
	h.validatorController = validatorController
	h.dutiesExecutor = dutiesExecutor
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

func (h *baseHandler) ctxWithDeadlineOnNextSlot(ctx context.Context, slot phase0.Slot) (context.Context, context.CancelFunc) {
	return h.ctxWithDeadlineOnSlot(ctx, slot+1)
}

func (h *baseHandler) ctxWithDeadlineInOneEpoch(ctx context.Context, slot phase0.Slot) (context.Context, context.CancelFunc) {
	// Attestation and aggregation submissions are rewarded as long as they are included within
	// SLOTS_PER_EPOCH slots of their target slot (i.e., from target slot up to and including target + SLOTS_PER_EPOCH).
	// See https://eth2book.info/latest/part2/incentives/rewards/#attestation-rewards
	// Sync committee duties have to use the same deadline because they are part of the committee role.
	// We set the deadline to target slot + SLOTS_PER_EPOCH + 1 (since the deadline slot itself is excluded).
	slotsPerEpoch := phase0.Slot(h.beaconConfig.SlotsPerEpoch)
	return h.ctxWithDeadlineOnSlot(ctx, slot+slotsPerEpoch+1)
}

// ctxWithDeadlineOnSlot returns the derived context with a deadline set to the beginning of the passed slot
// with some safety margin to account for clock skews.
func (h *baseHandler) ctxWithDeadlineOnSlot(ctx context.Context, slot phase0.Slot) (context.Context, context.CancelFunc) {
	return context.WithDeadline(ctx, h.beaconConfig.SlotStartTime(slot).Add(100*time.Millisecond))
}
