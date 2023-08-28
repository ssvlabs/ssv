package validator

import (
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

const bufSize = 1024

func newMessageRouter(logger *zap.Logger) *messageRouter {
	return &messageRouter{
		logger: logger,
		ch:     make(chan *queue.DecodedSSVMessage, bufSize),
		msgID:  commons.MsgID(),
	}
}

type messageRouter struct {
	logger *zap.Logger
	ch     chan *queue.DecodedSSVMessage
	msgID  commons.MsgIDFunc
}

func (r *messageRouter) Route(logger *zap.Logger, message *queue.DecodedSSVMessage) {
	select {
	case r.ch <- message:
	default:
		r.logger.Warn("message router buffer is full. dropping message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan *queue.DecodedSSVMessage {
	return r.ch
}
