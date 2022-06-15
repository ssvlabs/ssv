package changeround

import (
	"fmt"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
)

// ErrNotFound represents a not found error
var ErrNotFound = fmt.Errorf("not found")

// MsgHandler handles incoming change round messages
type MsgHandler func(*message.SignedMessage) error

// Fetcher is responsible for fetching change round messages from other peers in the subnet
type Fetcher interface {
	// GetChangeRoundMessages fetches change round messages for the given identifier and height
	GetChangeRoundMessages(identifier message.Identifier, height message.Height, handler MsgHandler) error
}

// changeRoundFetcher implements Fetcher
type changeRoundFetcher struct {
	logger *zap.Logger
	syncer p2pprotocol.Syncer
}

// NewLastRoundFetcher returns an instance of changeRoundFetcher
func NewLastRoundFetcher(logger *zap.Logger, syncer p2pprotocol.Syncer) Fetcher {
	return &changeRoundFetcher{
		logger: logger,
		syncer: syncer,
	}
}

func (crf *changeRoundFetcher) GetChangeRoundMessages(identifier message.Identifier, height message.Height, handler MsgHandler) error {
	msgs, err := crf.syncer.LastChangeRound(identifier, height)
	if err != nil {
		return errors.Wrap(err, "could not get change round messages")
	}

	logger := crf.logger.With(zap.String("identifier", identifier.String()), zap.Int64("height", int64(height)))

	logger.Debug("got last change round msgs", zap.Int("msgs count", len(msgs)), zap.Any("msgs", msgs))
	for _, msg := range msgs {
		syncMsg := &message.SyncMessage{}
		err = syncMsg.Decode(msg.Msg.Data)
		if err != nil {
			logger.Warn("could not decode change round message", zap.Error(err))
			continue
		}
		err = crf.msgError(syncMsg)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			logger.Warn("change round api error", zap.Error(err))
			continue
		}
		sm := syncMsg.Data[0]
		if err := handler(sm); err != nil {
			logger.Warn("could not handle message", zap.Error(err))
			continue
		}
	}
	return nil
}

func (crf *changeRoundFetcher) msgError(msg *message.SyncMessage) error {
	if msg == nil {
		return errors.New("msg is nil")
	} else if msg.Status == message.StatusNotFound {
		return ErrNotFound
	} else if msg.Status != message.StatusSuccess {
		return errors.Errorf("failed with status %d - %s", msg.Status, msg.Status.String())
	} else if len(msg.Data) != 1 { // TODO: extract to validation
		return errors.New("invalid result count")
	}
	return nil
}
