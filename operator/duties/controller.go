package duties

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/operator/slot_ticker"
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	prysmtypes "github.com/prysmaticlabs/eth2-types"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

//go:generate mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

// DutyExecutor represents the component that executes duties
type DutyExecutor interface {
	ExecuteDuty(duty *spectypes.Duty) error
}

// DutyController interface for dispatching duties execution according to slot ticker
type DutyController interface {
	Start()
}

// ControllerOptions holds the needed dependencies
type ControllerOptions struct {
	Logger              *zap.Logger
	Ctx                 context.Context
	BeaconClient        beaconprotocol.Beacon
	EthNetwork          beaconprotocol.Network
	ValidatorController validator.Controller
	Executor            DutyExecutor
	GenesisEpoch        uint64
	DutyLimit           uint64
	ForkVersion         forksprotocol.ForkVersion
	Ticker              slot_ticker.Ticker
}

// dutyController internal implementation of DutyController
type dutyController struct {
	logger     *zap.Logger
	ctx        context.Context
	ethNetwork beaconprotocol.Network
	// executor enables to work with a custom execution
	executor            DutyExecutor
	fetcher             DutyFetcher
	validatorController validator.Controller
	genesisEpoch        uint64
	dutyLimit           uint64
	ticker              slot_ticker.Ticker
}

var secPerSlot int64 = 12

// NewDutyController creates a new instance of DutyController
func NewDutyController(opts *ControllerOptions) DutyController {
	fetcher := newDutyFetcher(opts.Logger, opts.BeaconClient, opts.ValidatorController, opts.EthNetwork)
	dc := dutyController{
		logger:              opts.Logger,
		ctx:                 opts.Ctx,
		ethNetwork:          opts.EthNetwork,
		fetcher:             fetcher,
		validatorController: opts.ValidatorController,
		genesisEpoch:        opts.GenesisEpoch,
		dutyLimit:           opts.DutyLimit,
		executor:            opts.Executor,
		ticker:              opts.Ticker,
	}
	return &dc
}

// Start listens to slot ticker and dispatches duties execution
func (dc *dutyController) Start() {
	// warmup
	indices := dc.validatorController.GetValidatorsIndices()
	dc.logger.Debug("warming up indices", zap.Int("count", len(indices)))

	tickerChan := make(chan prysmtypes.Slot, 32)
	dc.ticker.Subscribe(tickerChan)
	dc.listenToTicker(tickerChan)
}

// ExecuteDuty tries to execute the given duty
func (dc *dutyController) ExecuteDuty(duty *spectypes.Duty) error {
	if dc.executor != nil {
		// enables to work with a custom executor, e.g. readOnlyDutyExec
		return dc.executor.ExecuteDuty(duty)
	}
	logger := dc.loggerWithDutyContext(dc.logger, duty)

	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer.
	// so we need to copy the pubkey val to avoid pointer
	var pk phase0.BLSPubKey
	copy(pk[:], duty.PubKey[:])

	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(pk[:]); err != nil {
		return errors.Wrap(err, "failed to deserialize pubkey from duty")
	}
	if v, ok := dc.validatorController.GetValidator(pubKey.SerializeToHexStr()); ok {
		ssvMsg, err := createDutyExecuteMsg(duty, pubKey)
		if err != nil {
			return err
		}
		dec, err := queue.DecodeSSVMessage(ssvMsg)
		if err != nil {
			return err
		}
		v.Queues[duty.Type].Q.Push(dec)
	} else {
		logger.Warn("could not find validator")
	}

	return nil
}

// createDutyExecuteMsg returns ssvMsg with event type of duty execute
func createDutyExecuteMsg(duty *spectypes.Duty, pubKey *bls.PublicKey) (*spectypes.SSVMessage, error) {
	executeDutyData := types.ExecuteDutyData{Duty: duty}
	edd, err := json.Marshal(executeDutyData)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal execute duty data")
	}
	msg := types.EventMsg{
		Type: types.ExecuteDuty,
		Data: edd,
	}
	data, err := msg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode event msg")
	}
	return &spectypes.SSVMessage{
		MsgType: message.SSVEventMsgType,
		MsgID:   spectypes.NewMsgID(pubKey.Serialize(), duty.Type),
		Data:    data,
	}, nil
}

// listenToTicker loop over the given slot channel
func (dc *dutyController) listenToTicker(slots <-chan prysmtypes.Slot) {
	for currentSlot := range slots {
		// execute duties
		if !dc.genesisEpochEffective() {
			continue
		}
		duties, err := dc.fetcher.GetDuties(uint64(currentSlot))
		if err != nil {
			dc.logger.Warn("failed to get duties", zap.Error(err))
		}
		for i := range duties {
			go dc.onDuty(&duties[i])
		}
	}
}

// onDuty handles next duty
func (dc *dutyController) onDuty(duty *spectypes.Duty) {
	logger := dc.loggerWithDutyContext(dc.logger, duty)
	if dc.shouldExecute(duty) {
		logger.Debug("duty was sent to execution")
		if err := dc.ExecuteDuty(duty); err != nil {
			logger.Warn("could not dispatch duty", zap.Error(err))
			return
		}
		return
	}
	logger.Warn("slot is irrelevant, ignoring duty")
}

func (dc *dutyController) genesisEpochEffective() bool {
	if dc.ethNetwork.EstimatedCurrentEpoch() < prysmtypes.Epoch(dc.genesisEpoch) {
		// wait until genesis epoch starts
		dc.logger.Debug("skipping slot, lower than genesis",
			zap.Uint64("genesis_slot", dc.getEpochFirstSlot(dc.genesisEpoch)),
			zap.Uint64("current_slot", uint64(dc.ethNetwork.EstimatedCurrentSlot())))
		return false
	}

	return true
}

func (dc *dutyController) shouldExecute(duty *spectypes.Duty) bool {
	currentSlot := uint64(dc.ethNetwork.EstimatedCurrentSlot())
	// execute task if slot already began and not pass 1 epoch
	if currentSlot >= uint64(duty.Slot) && currentSlot-uint64(duty.Slot) <= dc.dutyLimit {
		return true
	} else if currentSlot+1 == uint64(duty.Slot) {
		dc.loggerWithDutyContext(dc.logger, duty).Debug("current slot and duty slot are not aligned, "+
			"assuming diff caused by a time drift - ignoring and executing duty", zap.String("type", duty.Type.String()))
		return true
	}
	return false
}

// loggerWithDutyContext returns an instance of logger with the given duty's information
func (dc *dutyController) loggerWithDutyContext(logger *zap.Logger, duty *spectypes.Duty) *zap.Logger {
	currentSlot := uint64(dc.ethNetwork.EstimatedCurrentSlot())
	return logger.
		With(zap.String("role", duty.Type.String())).
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(zap.Uint64("current slot", currentSlot)).
		With(zap.Uint64("slot", uint64(duty.Slot))).
		With(zap.Uint64("epoch", uint64(duty.Slot)/32)).
		With(zap.String("pubKey", hex.EncodeToString(duty.PubKey[:]))).
		With(zap.Time("start_time", dc.ethNetwork.GetSlotStartTime(uint64(duty.Slot))))
}

// getEpochFirstSlot returns the beacon node first slot in epoch
func (dc *dutyController) getEpochFirstSlot(epoch uint64) uint64 {
	return epoch * 32
}

// NewReadOnlyExecutor creates a dummy executor that is used to run in read mode
func NewReadOnlyExecutor(logger *zap.Logger) DutyExecutor {
	return &readOnlyDutyExec{logger: logger}
}

type readOnlyDutyExec struct {
	logger *zap.Logger
}

func (e *readOnlyDutyExec) ExecuteDuty(duty *spectypes.Duty) error {
	e.logger.Debug("skipping duty execution",
		zap.Uint64("epoch", uint64(duty.Slot)/32),
		zap.Uint64("slot", uint64(duty.Slot)),
		zap.String("pubKey", hex.EncodeToString(duty.PubKey[:])))
	return nil
}
