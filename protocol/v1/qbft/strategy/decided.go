package strategy

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/sync"
	"go.uber.org/zap"
)

// Mode represents an internal mode of the node.
// from user POV it can configure it as a fullnode,
// but since v1 we will use a quiet fullnode for fullnodes.
type Mode int32

const (
	// ModeRegularNode is the regular mode, default for v1
	ModeRegularNode Mode = iota
	// ModeFullNode is a fullnode mode, default for v0
	ModeFullNode
)

// Decided helps to decouple regular from full-node mode where the node is saving decided history.
// in regular mode, the node only cares about last decided messages.
type Decided interface {
	// Sync performs a sync with the other peers in the network
	Sync(ctx context.Context, identifier message.Identifier, from, to *message.SignedMessage, pip pipelines.SignedMessagePipeline) error
	// UpdateDecided updates the given decided message
	UpdateDecided(msg *message.SignedMessage) (bool, error)
	// GetDecided returns historical decided messages
	GetDecided(identifier message.Identifier, heightRange ...message.Height) ([]*message.SignedMessage, error)
	// GetLastDecided returns height decided messages
	GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error)
	// SaveDecided saves the given decided messages
	SaveDecided(signedMsg ...*message.SignedMessage) (bool, error)
}

// SaveLastDecided saves last decided message if its height is larger than persisted height
func SaveLastDecided(logger *zap.Logger, store qbftstorage.DecidedMsgStore, signedMsgs ...*message.SignedMessage) (bool, error) {
	msg := sync.GetHighestSignedMessage(signedMsgs...)
	if msg == nil {
		return false, nil
	}
	local, err := store.GetLastDecided(msg.Message.Identifier)
	if err != nil {
		return false, err
	}
	// msg has lower or equal height
	if local != nil && !msg.Message.Higher(local.Message) {
		return false, nil
	}
	// msg has fewer signers
	if !msg.HasMoreSigners(local) {
		return false, nil
	}
	if err := store.SaveLastDecided(msg); err != nil {
		logger.Debug("could not save decided",
			zap.String("identifier", msg.Message.Identifier.String()),
			zap.Int64("height", int64(msg.Message.Height)),
			zap.Error(err))
		return false, err
	}
	return true, nil
}
