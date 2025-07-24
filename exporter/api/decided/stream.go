package decided

import (
	"fmt"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/api"
	"github.com/ssvlabs/ssv/logging/fields"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

// NewStreamPublisher handles incoming newly decided messages.
// it forward messages to websocket stream, where messages are cached (1m TTL) to avoid flooding
func NewStreamPublisher(logger *zap.Logger, domainType spectypes.DomainType, ws api.WebSocketServer) controller.NewDecidedHandler {
	c := cache.New(time.Minute, time.Minute*3/2)
	feed := ws.BroadcastFeed()
	return func(msg qbftstorage.Participation) {
		key := fmt.Sprintf("%x:%d:%d", msg.PubKey[:], msg.Slot, len(msg.Signers))
		_, ok := c.Get(key)
		if ok {
			return
		}
		c.SetDefault(key, true)

		logger.Debug("broadcast decided stream", fields.PubKey(msg.PubKey[:]), fields.Slot(msg.Slot))
		feed.Send(api.NewParticipantsAPIMsg(domainType, msg))
	}
}

type DecidedListener struct {
	feed       *event.Feed
	logger     *zap.Logger
	domainType spectypes.DomainType
	cache      *cache.Cache
}

func NewDecidedListener(logger *zap.Logger, domainType spectypes.DomainType, ws api.WebSocketServer) *DecidedListener {
	return &DecidedListener{
		feed:       ws.BroadcastFeed(),
		logger:     logger,
		domainType: domainType,
		cache:      cache.New(time.Minute, time.Minute*3/2),
	}
}

func (c *DecidedListener) OnDecided(msg dutytracer.DecidedInfo) {
	participation := qbftstorage.Participation{
		ParticipantsRangeEntry: qbftstorage.ParticipantsRangeEntry{
			PubKey:  msg.PubKey,
			Slot:    msg.Slot,
			Signers: msg.Signers,
		},
		Role:   msg.Role,
		PubKey: msg.PubKey,
	}

	key := fmt.Sprintf("%x:%d:%d", msg.PubKey[:], msg.Slot, len(msg.Signers))
	_, ok := c.cache.Get(key)
	if ok {
		return
	}
	c.logger.Info("DecidedListener: sending to websocket feed",
		zap.String("validator_pk", fmt.Sprintf("%x", msg.PubKey[:])),
		zap.String("signers", fmt.Sprintf("%v", msg.Signers)),
		zap.Uint64("slot", uint64(msg.Slot)),
		zap.String("role", msg.Role.String()),
	)
	c.cache.SetDefault(key, true)
	c.feed.Send(api.NewParticipantsAPIMsg(c.domainType, participation))
}
