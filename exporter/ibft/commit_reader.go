package ibft

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// CommitReaderOptions defines the required parameters to create an instance
type CommitReaderOptions struct {
	Logger           *zap.Logger
	Network          network.Network
	ValidatorStorage validatorstorage.ICollection
	IbftStorage      collections.Iibft
}

type commitReader struct {
	logger           *zap.Logger
	network          network.Network
	validatorStorage validatorstorage.ICollection
	ibftStorage      collections.Iibft
}

// NewCommitReader creates new instance
func NewCommitReader(opts CommitReaderOptions) Reader {
	r := &commitReader{
		logger:           opts.Logger.With(zap.String("who", "commit_reader")),
		network:          opts.Network,
		validatorStorage: opts.ValidatorStorage,
		ibftStorage:      opts.IbftStorage,
	}
	return r
}

// Start starts the reader
func (cr *commitReader) Start() error {
	cr.listenToNetwork(cr.network.ReceivedMsgChan())
	return nil
}

// listenToNetwork listens to commit messages
func (cr *commitReader) listenToNetwork(msgChan <-chan *proto.SignedMessage) {
	cr.logger.Debug("listening to network messages")
	for msg := range msgChan {
		if err := auth.BasicMsgValidation().Run(msg); err != nil {
			// received invalid msg
			continue
		}
		// filtering irrelevant messages
		if msg.Message.Type != proto.RoundState_Commit {
			continue
		}
		if err := cr.handleCommitMessage(msg); err != nil {
			cr.logger.Warn("could not handle commit message", zap.String("err", err.Error()))
		}
	}
}

// handleCommitMessage handles a new commit message
func (cr *commitReader) handleCommitMessage(msg *proto.SignedMessage) error {
	pkHex, _ := format.IdentifierUnformat(string(msg.Message.Lambda))
	logger := cr.logger.With(zap.String("pk", pkHex))
	pk, err := hex.DecodeString(pkHex)
	if err != nil {
		return errors.Wrap(err, "could not read public key")
	}
	share, found, err := cr.validatorStorage.GetValidatorShare(pk)
	if err != nil {
		return errors.Wrap(err, "could not get validator share")
	}
	if !found {
		logger.Debug("could not find share")
		return nil
	}
	if err := validateCommitMsg(msg, share); err != nil {
		logger.Debug("invalid commit message")
		return errors.Wrap(err, "invalid commit message")
	}
	return cr.onValidCommitMessage(msg)
}

// onValidCommitMessage
func (cr *commitReader) onValidCommitMessage(msg *proto.SignedMessage) error {
	pkHex, _ := format.IdentifierUnformat(string(msg.Message.Lambda))
	logger := cr.logger.With(zap.String("pk", pkHex))
	decided, found, err := cr.ibftStorage.GetDecided(msg.Message.Lambda, msg.Message.SeqNumber)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := decided.VerifyForAggregation(msg); err != nil {
		logger.Debug("not verified for aggregation", zap.String("why", err.Error()))
		return nil
	}
	if err = decided.Aggregate(msg); err != nil {
		return errors.Wrap(err, "could not aggregate commit message")
	}
	if err := cr.ibftStorage.SaveDecided(decided); err != nil {
		return errors.Wrap(err, "could not save aggregated decided message")
	}
	logger.Debug("decided message was updated", zap.Uint64("seq", decided.Message.SeqNumber))
	return nil
}

// validateCommitMsg validates commit message
func validateCommitMsg(msg *proto.SignedMessage, share *validatorstorage.Share) error {
	p := pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_Commit),
		auth.AuthorizeMsg(share),
		auth.ValidateQuorum(share.ThresholdSize()),
	)
	return p.Run(msg)
}
