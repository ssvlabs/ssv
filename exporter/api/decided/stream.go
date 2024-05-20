package decided

import (
	"encoding/hex"
	"fmt"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/patrickmn/go-cache"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/api"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
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
