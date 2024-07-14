package decided

import (
	"encoding/hex"
	"fmt"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"time"

	"github.com/patrickmn/go-cache"
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
	return func(msg qbftstorage.ParticipantsRangeEntry) {
		identifier := hex.EncodeToString(msg.Identifier.GetDutyExecutorID()[:])
		key := fmt.Sprintf("%s:%d:%d", identifier, msg.Slot, len(msg.Signers))
		_, ok := c.Get(key)
		if ok {
			return
		}
		c.SetDefault(key, true)

		logger.Debug("broadcast decided stream", zap.String("identifier", identifier), fields.Slot(msg.Slot))
		feed.Send(api.NewParticipantsAPIMsg(msg))
	}
}
