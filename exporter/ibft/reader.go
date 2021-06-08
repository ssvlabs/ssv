package ibft

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// Reader is a minimal interface for ibft in the context of an exporter
type Reader interface {
	Start()
	Sync() error
}

// ReaderOptions defines the required parameters to create an instance
type ReaderOptions struct {
	Logger         *zap.Logger
	Storage        collections.Iibft
	Network        network.Network
	Config         *proto.InstanceConfig
	ValidatorShare *storage.Share
}

type reader struct {
	logger  *zap.Logger
	storage collections.Iibft
	network network.Network

	config         *proto.InstanceConfig
	validatorShare *storage.Share
}

// NewIbftReadOnly creates  new instance of Reader
func NewIbftReadOnly(opts ReaderOptions) Reader {
	r := reader{
		logger:         opts.Logger,
		storage:        opts.Storage,
		network:        opts.Network,
		config:         opts.Config,
		validatorShare: opts.ValidatorShare,
	}
	return &r
}

// Sync will fetch best known decided message (highest sequence) from the network and sync to it.
func (r *reader) Sync() error {
	validatorPubkey := r.validatorShare.PublicKey.Serialize()
	// subscribe to topic so we could find relevant nodes
	err := r.network.SubscribeToValidatorNetwork(r.validatorShare.PublicKey, true)
	if err != nil {
		r.logger.Error("could not subscribe to validator channel", zap.Error(err),
			zap.String("validatorPubkey", r.validatorShare.PublicKey.SerializeToHexStr()))
	}
	time.Sleep(3500 * time.Millisecond) // wait for network setup
	s := sync.NewHistorySync(r.logger, validatorPubkey, r.network, r.storage, r.validateDecidedMsg)
	return s.Start()
}

// Start starts the network listeners
func (r *reader) Start() {
	r.listenToNetworkDecidedMessages()
}

// listenToNetworkDecidedMessages listens for decided messages
func (r *reader) listenToNetworkDecidedMessages() {
	decidedChan := r.network.ReceivedDecidedChan()
	go func() {
		for msg := range decidedChan {
			r.logger.Debug("received a new decided message")
			if err := r.validateDecidedMsg(msg); err != nil {
				r.logger.Error("received invalid decided message", zap.Error(err), zap.Uint64s("signer ids", msg.SignerIds))
			}
			err := r.processDecidedMessage(msg)
			if err != nil {
				r.logger.Debug("failed to process decided message")
			}
		}
	}()
}

// validateDecidedMsg validates the message
func (r *reader) validateDecidedMsg(msg *proto.SignedMessage) error {
	r.logger.Debug("validating a new decided message", zap.String("msg", msg.String()))
	p := pipeline.Combine(
		auth.MsgTypeCheck(proto.RoundState_Commit),
		auth.ValidatePKs(r.validatorShare.PublicKey.Serialize()),
		auth.AuthorizeMsg(r.validatorShare),
		auth.ValidateQuorum(r.validatorShare.ThresholdSize()),
	)
	return p.Run(msg)
}

// processDecidedMessage is responsible for processing an incoming decided message.
// If the decided message is known or belong to the current executing instance, do nothing.
// Else perform a sync operation
func (r *reader) processDecidedMessage(msg *proto.SignedMessage) error {
	r.logger.Debug("processing a valid decided message", zap.Uint64("seq number", msg.Message.SeqNumber), zap.Uint64s("signer ids", msg.SignerIds))

	// if we already have this in storage, pass
	known, err := r.decidedMsgKnown(msg)
	if err != nil {
		return errors.Wrap(err, "failed to check if decided msg is known")
	}
	if known {
		return nil
	}

	shouldSync, err := r.decidedRequiresSync(msg)
	if err != nil {
		return errors.Wrap(err, "failed to check if decided sync is required")
	}
	if shouldSync {
		r.logger.Warn("[not implemented yet] should sync validator data",
			zap.String("validatorPubkey", hex.EncodeToString(msg.Message.ValidatorPk)))
	}
	return nil
}

func (r *reader) decidedMsgKnown(msg *proto.SignedMessage) (bool, error) {
	found, err := r.storage.GetDecided(msg.Message.ValidatorPk, msg.Message.SeqNumber)
	if err != nil && err.Error() != collections.EntryNotFoundError {
		return false, errors.Wrap(err, "could not get decided instance from storage")
	}
	return found != nil, nil
}

// decidedRequiresSync returns true if:
// 		- highest known seq lower than msg seq
// 		- AND msg is not for current instance
func (r *reader) decidedRequiresSync(msg *proto.SignedMessage) (bool, error) {
	if msg.Message.SeqNumber == 0 {
		return false, nil
	}
	highest, err := r.storage.GetHighestDecidedInstance(msg.Message.ValidatorPk)
	if err != nil {
		if err.Error() == collections.EntryNotFoundError {
			return msg.Message.SeqNumber > 0, nil
		}
		return false, errors.Wrap(err, "could not get highest decided instance from storage")
	}
	return highest.Message.SeqNumber < msg.Message.SeqNumber, nil
}
