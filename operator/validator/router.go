package validator

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/patrickmn/go-cache"
	"go.uber.org/zap"
	"time"
)

const bufSize = 1024

func newMessageRouter(logger *zap.Logger, msgID forks.MsgIDFunc) *messageRouter {
	return &messageRouter{
		logger: logger,
		ch:     make(chan message.SSVMessage, bufSize),
		cache:  cache.New(8*time.Minute, 10*time.Minute),
		msgID:  msgID,
	}
}

type messageRouter struct {
	logger *zap.Logger
	ch     chan message.SSVMessage
	cache  *cache.Cache
	msgID  forks.MsgIDFunc
}

func (r *messageRouter) Route(message message.SSVMessage) {
	if !r.checkCache(&message) {
		return
	}
	select {
	case r.ch <- message:
	default:
		r.logger.Warn("message router buffer is full. dropping message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan message.SSVMessage {
	return r.ch
}

func (r *messageRouter) checkCache(msg *message.SSVMessage) bool {
	data, err := msg.Encode()
	if err != nil {
		return false
	}
	mid := r.msgID(data)
	if _, ok := r.cache.Get(mid); ok {
		return false
	}
	r.cache.SetDefault(mid, true)
	return true
}
