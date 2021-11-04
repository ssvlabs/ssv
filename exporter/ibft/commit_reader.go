package ibft

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/beacon"
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/pubsub"
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
	Out              pubsub.EventPublisher
}

// commitReader responsible for reading all commit messages
// it will try to aggregate existing decided message to make sure all participating operators are listed
type commitReader struct {
	logger           *zap.Logger
	network          network.Network
	validatorStorage validatorstorage.ICollection
	ibftStorage      collections.Iibft
	out              pubsub.EventPublisher
}

// NewCommitReader creates new instance
func NewCommitReader(opts CommitReaderOptions) Reader {
	r := &commitReader{
		logger:           opts.Logger.With(zap.String("who", "commit_reader")),
		network:          opts.Network,
		validatorStorage: opts.ValidatorStorage,
		ibftStorage:      opts.IbftStorage,
		out:              opts.Out,
	}
	return r
}

// Start starts the reader
func (cr *commitReader) Start() error {
	msgCn := cr.network.ReceivedMsgChan()
	cr.logger.Debug("listening to network messages")
	for msg := range msgCn {
		if processed := cr.onMessage(msg); processed {
			cr.logger.Debug("managed to process commit message",
				zap.String("", string(msg.Message.Lambda)), zap.Uint64("seq", msg.Message.SeqNumber))
		}
	}
	return nil
}

func (cr *commitReader) onMessage(msg *proto.SignedMessage) bool {
	if err := auth.BasicMsgValidation().Run(msg); err != nil {
		// received invalid msg
		return false
	}
	// filtering irrelevant messages
	if msg.Message.Type != proto.RoundState_Commit {
		return false
	}
	if err := cr.onCommitMessage(msg); err != nil {
		cr.logger.Debug("could not handle commit message", zap.String("err", err.Error()))
	}
	return true
}

// onCommitMessage handles a new commit message
func (cr *commitReader) onCommitMessage(msg *proto.SignedMessage) error {
	pkHex, _ := format.IdentifierUnformat(string(msg.Message.Lambda))
	logger := cr.logger.With(zap.String("pk", pkHex), zap.Uint64("seq", msg.Message.SeqNumber))
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
		return errors.Wrap(err, "invalid commit message")
	}
	updated, err := ibft2.ProcessLateCommitMsg(msg, cr.ibftStorage, pkHex)
	if err != nil {
		return errors.Wrap(err, "failed to process late commit message")
	}
	if updated {
		logger.Debug("decided message was updated")
		go cr.out.Notify("out", newDecidedNetworkMsg(msg, pkHex))
	}
	return nil
}

// validateCommitMsg validates commit message
func validateCommitMsg(msg *proto.SignedMessage, share *validatorstorage.Share) error {
	identifier := []byte(format.IdentifierFormat(share.PublicKey.Serialize(), beacon.RoleTypeAttester.String()))
	p := pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_Commit),
		auth.ValidateLambdas(identifier),
		auth.AuthorizeMsg(share),
	)
	return p.Run(msg)
}
