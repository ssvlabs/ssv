package decided

import (
	"fmt"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/patrickmn/go-cache"
	"go.uber.org/zap"
	"time"
)

// NewStreamPublisher handles incoming newly decided messages.
// it forward messages to websocket stream, where messages are cached (1m TTL) to avoid flooding
func NewStreamPublisher(logger *zap.Logger, ws api.WebSocketServer) controller.NewDecidedHandler {
	logger = logger.With(zap.String("who", "NewDecidedHandler"))
	c := cache.New(time.Minute, time.Minute*3/2)
	feed := ws.BroadcastFeed()
	return func(msg *message.SignedMessage) {
		identifier := msg.Message.Identifier.String()
		key := fmt.Sprintf("%s:%d:%d", msg.Message.Identifier.String(), msg.Message.Height, len(msg.Signers))
		_, ok := c.Get(key)
		if ok {
			return
		}
		c.SetDefault(key, true)
		logger.Debug("broadcast decided stream",
			zap.String("identifier", identifier),
			zap.Uint64("height", uint64(msg.Message.Height)))
		apiMsg, err := api.NewDecidedAPIMsg(msg)
		if err != nil {
			logger.Warn("could not create new decided api message", zap.Error(err))
		}
		feed.Send(apiMsg)
	}
}
