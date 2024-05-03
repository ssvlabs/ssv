package validator

import (
	"context"
	"fmt"
	"sync"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/message/msgvalidation"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	mtx    *sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	DutyRunners runner.DutyRunners
	Network     specqbft.Network
	Operator    *spectypes.Operator
	Share       *types.SSVShare

	Signer         spectypes.BeaconSigner
	OperatorSigner spectypes.OperatorSigner

	Storage *storage.QBFTStores
	Queues  map[types.RunnerRole]queueContainer

	// dutyIDs is a map for logging a unique ID for a given duty
	dutyIDs *hashmap.Map[types.RunnerRole, string]

	state uint32

	messageValidator msgvalidation.MessageValidator
}

// NewValidator creates a new instance of Validator.
func NewValidator(pctx context.Context, cancel func(), options Options) *Validator {
	options.defaults()

	if options.Metrics == nil {
		options.Metrics = &NopMetrics{}
	}

	v := &Validator{
		mtx:              &sync.RWMutex{},
		ctx:              pctx,
		cancel:           cancel,
		DutyRunners:      options.DutyRunners,
		Network:          options.Network,
		Storage:          options.Storage,
		Share:            options.SSVShare,
		Signer:           options.Signer,
		OperatorSigner:   options.OperatorSigner,
		Queues:           make(map[types.RunnerRole]queueContainer),
		state:            uint32(NotStarted),
		dutyIDs:          hashmap.New[types.RunnerRole, string](),
		messageValidator: options.MessageValidator,
	}

	for _, dutyRunner := range options.DutyRunners {
		// Set timeout function.
		dutyRunner.GetBaseRunner().TimeoutF = v.onTimeout

		// Setup the queue.
		v.Queues[types.RunnerRoleFromSpec(dutyRunner.GetRunnerRole())] = queueContainer{
			Q: queue.WithMetrics(queue.New(options.QueueSize), options.Metrics),
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             0,
				Slot:               0,
				Quorum:             options.SSVShare.GetQuorum(),
			},
		}
	}

	return v
}

// StartDuty starts a duty for the validator
func (v *Validator) StartDuty(logger *zap.Logger, duty spectypes.Duty) error {
	beaconDuty, ok := duty.(*spectypes.BeaconDuty)
	if !ok {
		return errors.New("duty is not a beacon duty")
	}
	dutyRunner := v.DutyRunners[types.RunnerRoleFromBeacon(beaconDuty.Type)]
	if dutyRunner == nil {
		return errors.Errorf("no runner for duty type %s", beaconDuty.Type.String())
	}

	// Log with duty ID.
	baseRunner := dutyRunner.GetBaseRunner()
	v.dutyIDs.Set(types.RunnerRoleFromBeacon(beaconDuty.Type),
		fields.FormatDutyID(baseRunner.BeaconNetwork.EstimatedEpochAtSlot(beaconDuty.Slot), duty))
	logger = trySetDutyID(logger, v.dutyIDs, types.RunnerRoleFromBeacon(beaconDuty.Type))

	// Log with height.
	if baseRunner.QBFTController != nil {
		logger = logger.With(fields.Height(baseRunner.QBFTController.Height))
	}

	logger.Info("ℹ️ starting duty processing")

	return dutyRunner.StartNewDuty(logger, duty)
}

// ProcessMessage processes Network Message of all types
func (v *Validator) ProcessMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) error {
	messageID := msg.GetID()
	dutyRunner := v.DutyRunners.ByMessageID(messageID)
	if dutyRunner == nil {
		return fmt.Errorf("could not get duty runner for msg ID %v", messageID)
	}

	if err := validateMessage(v.Share.Share, msg); err != nil {
		return fmt.Errorf("message invalid for msg ID %v: %w", messageID, err)
	}

	// TODO: fork support
	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		logger = trySetDutyID(logger, v.dutyIDs, types.RunnerRoleFromSpec(messageID.GetRoleType()))

		qbftMsg, ok := msg.Body.(*specqbft.Message)
		if !ok {
			return errors.New("could not decode consensus message from network message")
		}
		logger = logger.With(fields.Height(qbftMsg.Height))
		return dutyRunner.ProcessConsensus(logger, msg.SignedSSVMessage)
	case spectypes.SSVPartialSignatureMsgType:
		logger = trySetDutyID(logger, v.dutyIDs, types.RunnerRoleFromSpec(messageID.GetRoleType()))

		partSigMsg, ok := msg.Body.(*spectypes.PartialSignatureMessages)
		if !ok {
			return errors.New("could not decode post consensus message from network message")
		}
		if partSigMsg.Type == spectypes.PostConsensusPartialSig {
			return dutyRunner.ProcessPostConsensus(logger, partSigMsg)
		}
		return dutyRunner.ProcessPreConsensus(logger, partSigMsg)
	case message.SSVEventMsgType:
		return v.handleEventMessage(logger, msg, dutyRunner)
	default:
		return errors.New("unknown msg")
	}
}

func (v *Validator) SenderID() []byte {
	return v.Share.ValidatorPubKey[:]
}

func (v *Validator) PushMessage(msg *queue.DecodedSSVMessage) {
	v.ProcessMessage(zap.L(), msg)
}

func (v *Validator) UpdateMetadata(metadata *beaconprotocol.ValidatorMetadata) {

}

func validateMessage(share spectypes.Share, msg *queue.DecodedSSVMessage) error {
	if !share.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}

func trySetDutyID(logger *zap.Logger, dutyIDs *hashmap.Map[types.RunnerRole, string], role types.RunnerRole) *zap.Logger {
	if dutyID, ok := dutyIDs.Get(role); ok {
		return logger.With(fields.DutyID(dutyID))
	}
	return logger
}
