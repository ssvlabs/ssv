package ibft

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type IbftReadOnly interface {
	Start()
	Sync()
}

type IbftReadOnlyOptions struct {
	Logger         *zap.Logger
	IbftStorage    collections.Iibft
	Network        network.Network
	Params         *proto.InstanceParams
	ValidatorShare *storage.Share
}

type ibftReadOnly struct {
	logger      *zap.Logger
	ibftStorage collections.Iibft
	network     network.Network

	params         *proto.InstanceParams
	validatorShare *storage.Share
}

func NewIbftReadOnly(opts IbftReadOnlyOptions) IbftReadOnly {
	i := ibftReadOnly{
		logger: opts.Logger,
		ibftStorage: opts.IbftStorage,
		network: opts.Network,
		params: opts.Params,
		validatorShare: opts.ValidatorShare,
	}
	return &i
}

// Sync will fetch best known decided message (highest sequence) from the network and sync to it.
func (i *ibftReadOnly) Sync() {
	i.sync(i.validatorShare.PublicKey.Serialize())
}

func (i *ibftReadOnly) Start() {
	i.listenToNetworkDecidedMessages()
}

func (i *ibftReadOnly) listenToNetworkDecidedMessages() {
	decidedChan := i.network.ReceivedDecidedChan()
	go func() {
		for msg := range decidedChan {
			i.logger.Debug("received a new decided message")
			if err := i.validateDecidedMsg(msg); err != nil {
				i.logger.Error("received invalid decided message", zap.Error(err), zap.Uint64s("signer ids", msg.SignerIds))
			}
			err := i.processDecidedMessage(msg)
			if err != nil {
				i.logger.Debug("failed to process decided message")
			}
		}
	}()
}

func (i *ibftReadOnly) sync(validatorPubkey []byte) {
	if validatorPubkey == nil || len(validatorPubkey) == 0 {
		validatorPubkey = i.validatorShare.PublicKey.Serialize()
	}
	s := sync.NewHistorySync(i.logger, validatorPubkey, i.network, i.ibftStorage, i.validateDecidedMsg)
	s.Start()
}

// validateDecidedMsg validates the message
func (i *ibftReadOnly) validateDecidedMsg(msg *proto.SignedMessage) error {
	i.logger.Debug("validating a new decided message", zap.String("msg", msg.String()))
	p := pipeline.Combine(
		//decided.PrevInstanceDecided(prevInstanceStatus == proto.RoundState_Decided),
		auth.MsgTypeCheck(proto.RoundState_Commit),
		//auth.ValidateLambdas(msg.Message.Lambda, expectedPrevIdentifier),
		auth.ValidatePKs(i.validatorShare.PublicKey.Serialize()),
		auth.AuthorizeMsg(i.params),
		auth.ValidateQuorum(i.params.ThresholdSize()),
	)
	return p.Run(msg)
}

// processDecidedMessage is responsible for processing an incoming decided message.
// If the decided message is known or belong to the current executing instance, do nothing.
// Else perform a sync operation
func (i *ibftReadOnly) processDecidedMessage(msg *proto.SignedMessage) error {
	i.logger.Debug("processing a valid decided message", zap.Uint64("seq number", msg.Message.SeqNumber), zap.Uint64s("signer ids", msg.SignerIds))

	// if we already have this in storage, pass
	known, err := i.decidedMsgKnown(msg)
	if err != nil {
		return errors.Wrap(err, "failed to check if decided msg is known")
	}
	if known {
		return nil
	}

	shouldSync, err := i.decidedRequiresSync(msg)
	if err != nil {
		return errors.Wrap(err, "failed to check if decided sync is required")
	}
	if shouldSync {
		i.sync(msg.Message.ValidatorPk)
	}
	return nil
}

func (i *ibftReadOnly) decidedMsgKnown(msg *proto.SignedMessage) (bool, error) {
	found, err := i.ibftStorage.GetDecided(msg.Message.ValidatorPk, msg.Message.SeqNumber)
	if err != nil && err.Error() != collections.EntryNotFoundError {
		return false, errors.Wrap(err, "could not get decided instance from storage")
	}
	return found != nil, nil
}

// decidedRequiresSync returns true if:
// 		- highest known seq lower than msg seq
// 		- AND msg is not for current instance
func (i *ibftReadOnly) decidedRequiresSync(msg *proto.SignedMessage) (bool, error) {
	if msg.Message.SeqNumber == 0 {
		return false, nil
	}
	highest, err := i.ibftStorage.GetHighestDecidedInstance(msg.Message.ValidatorPk)
	if err != nil {
		if err.Error() == collections.EntryNotFoundError {
			return msg.Message.SeqNumber > 0, nil
		}
		return false, errors.Wrap(err, "could not get highest decided instance from storage")
	}
	return highest.Message.SeqNumber < msg.Message.SeqNumber, nil
}
