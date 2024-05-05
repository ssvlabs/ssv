package decided

import (
	"encoding/hex"
	"fmt"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"time"

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
	return func(signedMsg *spectypes.SignedSSVMessage) {

		msg, err := specqbft.DecodeMessage(signedMsg.SSVMessage.Data)
		if err != nil {
			return
		}

		identifier := hex.EncodeToString(msg.Identifier)
		key := fmt.Sprintf("%s:%d:%d", identifier, msg.Height, len(signedMsg.OperatorIDs))
		_, ok := c.Get(key)
		if ok {
			return
		}
		c.SetDefault(key, true)

		logger.Debug("broadcast decided stream", zap.String("identifier", identifier), fields.Height(msg.Height))

		feed.Send(api.NewDecidedAPIMsg(signedMsg))
	}
}
