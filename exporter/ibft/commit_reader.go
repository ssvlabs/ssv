package ibft

import (
	"encoding/hex"
	ibftinstance "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	validatorstorage "github.com/bloxapp/ssv/operator/validator"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"
)

// CommitReaderOptions defines the required parameters to create an instance
type CommitReaderOptions struct {
	Logger           *zap.Logger
	Network          network.Network
	ValidatorStorage validatorstorage.ICollection
	IbftStorage      collections.Iibft
	Out              *event.Feed
}

// commitReader responsible for reading all commit messages
// it will try to aggregate existing decided message to make sure all participating operators are listed
type commitReader struct {
	logger           *zap.Logger
	network          network.Network
	validatorStorage validatorstorage.ICollection
	ibftStorage      collections.Iibft
	out              *event.Feed
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
	msgCn, done := cr.network.ReceivedMsgChan()
	defer done()
	cr.logger.Debug("listening to network messages")
	for msg := range msgCn {
		if processed := cr.onMessage(msg); processed {
			cr.logger.Debug("got valid commit message",
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
	go func() {
		if err := cr.onCommitMessage(msg); err != nil {
			cr.logger.Debug("could not handle commit message", zap.String("err", err.Error()))
		}
	}()
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
	// TODO: change to fork
	err = ibftinstance.CommitMsgValidationPipelineV0(msg.Message.Lambda, msg.Message.SeqNumber, share).Run(msg)
	if err != nil {
		return errors.Wrap(err, "invalid commit message")
	}
	updated, err := ibftinstance.ProcessLateCommitMsg(msg, cr.ibftStorage, share)
	if err != nil {
		return errors.Wrap(err, "failed to process late commit message")
	}
	if updated != nil {
		logger.Debug("decided message was updated")
		go cr.out.Send(newDecidedAPIMsg(updated, pkHex))
	}
	return nil
}
