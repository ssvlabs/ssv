package validator

import (
	"context"
	"fmt"
	"sync"

	"github.com/cornelk/hashmap"
	"github.com/pkg/errors"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv/ibft/genesisstorage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/message/validation"
	genesisrunner "github.com/ssvlabs/ssv/protocol/genesis/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	mtx    *sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	DutyRunners genesisrunner.DutyRunners
	Network     genesisspecqbft.Network
	Share       *types.SSVShare
	Signer      genesisspectypes.KeyManager

	Storage *genesisstorage.QBFTStores
	Queues  map[genesisspectypes.BeaconRole]queueContainer

	// dutyIDs is a map for logging a unique ID for a given duty
	dutyIDs *hashmap.Map[genesisspectypes.BeaconRole, string]

	state uint32

	messageValidator validation.MessageValidator
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
		Queues:           make(map[genesisspectypes.BeaconRole]queueContainer),
		state:            uint32(NotStarted),
		dutyIDs:          hashmap.New[genesisspectypes.BeaconRole, string](),
		messageValidator: options.MessageValidator,
	}

	for _, dutyRunner := range options.DutyRunners {
		// Set timeout function.
		dutyRunner.GetBaseRunner().TimeoutF = v.onTimeout

		// Setup the queue.
		role := dutyRunner.GetBaseRunner().BeaconRoleType

		v.Queues[role] = queueContainer{
			Q: queue.WithMetrics(queue.New(options.QueueSize), options.Metrics),
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             0,
				Slot:               0,
				//Quorum:             options.SSVShare.Share,// TODO
			},
		}
	}

	return v
}

// StartDuty starts a duty for the validator
func (v *Validator) StartDuty(logger *zap.Logger, duty *genesisspectypes.Duty) error {
	dutyRunner := v.DutyRunners[duty.Type]
	if dutyRunner == nil {
		return errors.Errorf("no runner for duty type %s", duty.Type.String())
	}

	// Log with duty ID.
	baseRunner := dutyRunner.GetBaseRunner()
	v.dutyIDs.Set(duty.Type, fields.FormatGenesisDutyID(baseRunner.BeaconNetwork.EstimatedEpochAtSlot(duty.Slot), duty))
	logger = trySetDutyID(logger, v.dutyIDs, duty.Type)

	// Log with height.
	if baseRunner.QBFTController != nil {
		logger = logger.With(fields.Height(specqbft.Height(baseRunner.QBFTController.Height)))
	}

	logger.Info("ℹ️ starting duty processing")

	return dutyRunner.StartNewDuty(logger, duty, v.Share.Quorum())
}

// ProcessMessage processes Network Message of all types
func (v *Validator) ProcessMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) error {
	messageID := msg.GetID()
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(genesisspectypes.MessageID(messageID))
	if dutyRunner == nil {
		return fmt.Errorf("could not get duty runner for msg ID %v", messageID)
	}

	if err := validateMessage(v.Share.Share, msg); err != nil {
		return fmt.Errorf("message invalid for msg ID %v: %w", messageID, err)
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		logger = trySetDutyID(logger, v.dutyIDs, ((*genesisspectypes.MessageID)(msg.MsgID[:])).GetRoleType())

		signedMsg, ok := msg.Body.(*genesisspecqbft.SignedMessage)
		if !ok {
			return errors.New("could not decode consensus message from network message")
		}
		logger = logger.With(fields.Height(specqbft.Height(signedMsg.Message.Height)))
		return dutyRunner.ProcessConsensus(logger, signedMsg)
	case spectypes.SSVPartialSignatureMsgType:
		logger = trySetDutyID(logger, v.dutyIDs, ((*genesisspectypes.MessageID)(msg.MsgID[:])).GetRoleType())

		signedMsg, ok := msg.Body.(*genesisspectypes.SignedPartialSignatureMessage)
		if !ok {
			return errors.New("could not decode post consensus message from network message")
		}
		if signedMsg.Message.Type == genesisspectypes.PostConsensusPartialSig {
			return dutyRunner.ProcessPostConsensus(logger, signedMsg)
		}
		return dutyRunner.ProcessPreConsensus(logger, signedMsg)
	case message.SSVEventMsgType:
		return v.handleEventMessage(logger, msg, dutyRunner)
	default:
		return errors.New("unknown msg")
	}
}

func validateMessage(share spectypes.Share, msg *queue.DecodedSSVMessage) error {
	if !share.ValidatorPubKey.MessageIDBelongs(spectypes.MessageID(msg.GetID())) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}

func trySetDutyID(logger *zap.Logger, dutyIDs *hashmap.Map[genesisspectypes.BeaconRole, string], role genesisspectypes.BeaconRole) *zap.Logger {
	if dutyID, ok := dutyIDs.Get(role); ok {
		return logger.With(fields.DutyID(dutyID))
	}
	return logger
}
