package strategy

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"go.uber.org/zap"
)

// Decided helps to decouple regular from full-node mode where the node is saving decided history.
// in regular mode, the node only cares about last decided messages.
type Decided interface {
	// Sync performs a sync with the other peers in the network
	Sync(ctx context.Context, identifier message.Identifier, pip pipelines.SignedMessagePipeline) error
	// ValidateHeight validates the height of the given message
	ValidateHeight(msg *message.SignedMessage) (bool, error)
	// IsMsgKnown checks if the given decided message is known
	IsMsgKnown(msg *message.SignedMessage) (bool, *message.SignedMessage, error)
	// SaveLateCommit saves a commit message that arrived late
	SaveLateCommit(msg *message.SignedMessage) error
	// UpdateDecided updates the given decided message
	UpdateDecided(msg *message.SignedMessage) error
	// GetDecided returns historical decided messages
	GetDecided(identifier message.Identifier, heightRange ...message.Height) ([]*message.SignedMessage, error)
	// GetLastDecided returns height decided messages
	GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error)
	// SaveDecided saves historical decided messages
	SaveDecided(signedMsg ...*message.SignedMessage) error
}

// SaveLastDecided saves last decided message if its height is larger than persisted height
func SaveLastDecided(logger *zap.Logger, store qbftstorage.DecidedMsgStore, signedMsgs ...*message.SignedMessage) error {
	for _, msg := range signedMsgs {
		last, err := store.GetLastDecided(msg.Message.Identifier)
		if err != nil {
			return err
		}
		if last != nil && last.Message.Height > msg.Message.Height {
			logger.Debug("skipping decided with lower height",
				zap.String("identifier", msg.Message.Identifier.String()),
				zap.Int64("height", int64(msg.Message.Height)))
			continue
		}
		if err := store.SaveLastDecided(msg); err != nil {
			logger.Debug("could not save decided",
				zap.String("identifier", msg.Message.Identifier.String()),
				zap.Int64("height", int64(msg.Message.Height)),
				zap.Error(err))
			return err
		}
		logger.Debug("saved decided", zap.String("identifier", msg.Message.Identifier.String()),
			zap.Int64("height", int64(msg.Message.Height)))
	}
	return nil
}
