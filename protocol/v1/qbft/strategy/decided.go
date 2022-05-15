package strategy

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// Decided helps to decouple regular from full-node mode where the node is saving decided history.
// in regular mode, the node only cares about last decided messages.
type Decided interface {
	// Sync performs a sync with the other peers in the network
	Sync(ctx context.Context, identifier message.Identifier, pip pipelines.SignedMessagePipeline) (*message.SignedMessage, error)
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
	// SaveDecided saves historical decided messages
	SaveDecided(signedMsg ...*message.SignedMessage) error
}
