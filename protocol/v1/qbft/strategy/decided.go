package strategy

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/sync"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Mode represents an internal mode of the node.
// from user POV it can configure it as a fullnode,
// but since v1 we will use a quiet fullnode for fullnodes.
type Mode int32

const (
	// ModeLightNode is the light mode, default for v1
	ModeLightNode Mode = iota
	// ModeFullNode is a full node mode, default for v0
	ModeFullNode
)

// Decided helps to decouple light from full-node mode where the node is saving decided history.
// in light mode, the node doesn't save history, only last/highest decided messages.
type Decided interface {
	// Sync performs a sync with the other peers in the network
	Sync(ctx context.Context, identifier message.Identifier, from, to *message.SignedMessage, pip pipelines.SignedMessagePipeline) error
	// UpdateDecided updates the given decided message and returns the updated version (could include new signers)
	UpdateDecided(msg *message.SignedMessage) (*message.SignedMessage, error)
	// GetDecided returns historical decided messages
	GetDecided(identifier message.Identifier, heightRange ...message.Height) ([]*message.SignedMessage, error)
	// GetLastDecided returns height decided messages
	GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error)
}

// UpdateLastDecided saves last decided message if its height is larger than persisted height
func UpdateLastDecided(logger *zap.Logger, store qbftstorage.DecidedMsgStore, signedMsgs ...*message.SignedMessage) (*message.SignedMessage, error) {
	highest := sync.GetHighestSignedMessage(signedMsgs...)
	//logger.Debug("updating last decided", zap.Any("highest", highest))
	if highest == nil {
		return nil, nil
	}
	local, err := store.GetLastDecided(highest.Message.Identifier)
	if err != nil {
		return nil, err
	}
	if local == nil {
		// should create
	} else if local.Message.Higher(highest.Message) {
		return nil, nil
	} else if highest.Message.Height == local.Message.Height {
		msg, ok := CheckSigners(local, highest)
		if !ok {
			return nil, nil
		}
		highest = msg
	}
	logger = logger.With(zap.Int64("height", int64(highest.Message.Height)),
		zap.String("identifier", highest.Message.Identifier.String()), zap.Any("signers", highest.Signers))
	if err := store.SaveLastDecided(highest); err != nil {
		return highest, errors.Wrap(err, "could not save last decided")
	}
	logger.Debug("saved last decided")
	return highest, nil
}

// CheckSigners will return the decided message with more signers if both are with the same height
func CheckSigners(local, msg *message.SignedMessage) (*message.SignedMessage, bool) {
	if local == nil {
		return msg, true
	}
	if msg.Message.Height == local.Message.Height && len(local.Signers) < len(msg.Signers) {
		return msg, true
	}
	return local, false
}
