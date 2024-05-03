package decided

import (
	"encoding/hex"
	"fmt"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/patrickmn/go-cache"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
)

// NewStreamPublisher handles incoming newly decided messages.
// it forward messages to websocket stream, where messages are cached (1m TTL) to avoid flooding
func NewStreamPublisher(logger *zap.Logger, ws api.WebSocketServer) controller.NewDecidedHandler {
	c := cache.New(time.Minute, time.Minute*3/2)
	feed := ws.BroadcastFeed()
	return func(msg *spectypes.SignedSSVMessage) {
		qbftMsg, err := specqbft.DecodeMessage(msg.SSVMessage.Data)
		if err != nil {
			return
		}

		identifier := hex.EncodeToString(qbftMsg.Identifier)
		key := fmt.Sprintf("%s:%d:%d", identifier, qbftMsg.Height, len(msg.GetOperatorIDs()))
		_, ok := c.Get(key)
		if ok {
			return
		}
		c.SetDefault(key, true)

		logger.Debug("broadcast decided stream", zap.String("identifier", identifier), fields.Height(qbftMsg.Height))

		feed.Send(api.NewDecidedAPIMsg(msg))
	}
}
