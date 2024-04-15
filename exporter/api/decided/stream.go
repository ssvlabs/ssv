package decided

import (
	"encoding/hex"
	"fmt"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/patrickmn/go-cache"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

// NewStreamPublisher handles incoming newly decided messages.
// it forward messages to websocket stream, where messages are cached (1m TTL) to avoid flooding
func NewStreamPublisher(logger *zap.Logger, ws api.WebSocketServer) controller.NewDecidedHandler {
	c := cache.New(time.Minute, time.Minute*3/2)
	feed := ws.BroadcastFeed()
	return func(msg *specqbft.SignedMessage) {
		identifier := hex.EncodeToString(msg.Message.Identifier)
		key := fmt.Sprintf("%s:%d:%d", identifier, msg.Message.Height, len(msg.Signers))
		_, ok := c.Get(key)
		if ok {
			return
		}
		c.SetDefault(key, true)

		logger.Debug("broadcast decided stream", zap.String("identifier", identifier), fields.Height(msg.Message.Height))

		feed.Send(api.NewDecidedAPIMsg(msg))
	}
}

func NewStreamPublisherParticipants(logger *zap.Logger, ws api.WebSocketServer) controller.NewParticipantsHandler {
	c := cache.New(time.Minute, time.Minute*3/2)
	feed := ws.BroadcastFeed()
	return func(msg qbftstorage.ParticipantsRangeEntry) {
		identifier := hex.EncodeToString(msg.Identifier[:])
		key := fmt.Sprintf("%s:%d:%d", identifier, msg.Slot, len(msg.Operators))
		_, ok := c.Get(key)
		if ok {
			return
		}
		c.SetDefault(key, true)

		logger.Debug("broadcast participants stream", zap.String("identifier", identifier), fields.Slot(msg.Slot))

		feed.Send(api.NewParticipantsAPIMsg(msg))
	}
}
