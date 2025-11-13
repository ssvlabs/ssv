package decided

import (
	"fmt"
	"time"

	"github.com/patrickmn/go-cache"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/api"
	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/observability/log/fields"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
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

// NewDecidedListener handles incoming newly decided messages.
// it forward messages to websocket stream, where messages are cached (1m TTL) to avoid flooding
func NewDecidedListener(logger *zap.Logger, domainType spectypes.DomainType, ws api.WebSocketServer, validators registrystorage.ValidatorStore) func(dutytracer.DecidedInfo) {
	feed := ws.BroadcastFeed()
	cache := cache.New(time.Minute, 90*time.Second) // 1m TTL, 1.5m eviction to avoid flooding ws stream

	return func(msg dutytracer.DecidedInfo) {
		// backfill pubkey for retro-compatibility until we deprecate it
		pk, found := validators.ValidatorPubkey(msg.Index)
		if !found {
			logger.Debug("decided listener: validator not found by index", fields.ValidatorIndex(msg.Index))
			return
		}

		participation := qbftstorage.Participation{
			ParticipantsRangeEntry: qbftstorage.ParticipantsRangeEntry{
				PubKey:  pk,
				Slot:    msg.Slot,
				Signers: msg.Signers,
			},
			Role:   msg.Role,
			PubKey: pk,
		}

		key := fmt.Sprintf("%d:%d:%d", msg.Index, msg.Slot, len(msg.Signers))
		_, ok := cache.Get(key)
		if ok {
			// already sent in the last minute, skipping to avoid flooding ws stream
			return
		}
		cache.SetDefault(key, true)
		feed.Send(api.NewParticipantsAPIMsg(domainType, participation))
	}
}
