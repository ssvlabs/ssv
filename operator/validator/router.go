package validator

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/commons"
)

const bufSize = 1024

func newMessageRouter() *messageRouter {
	return &messageRouter{
		ch:    make(chan spectypes.SSVMessage, bufSize),
		msgID: commons.MsgID(),
	}
}

type messageRouter struct {
	ch    chan spectypes.SSVMessage
	msgID commons.MsgIDFunc
}

func (r *messageRouter) Route(logger *zap.Logger, message spectypes.SSVMessage) {
	select {
	case r.ch <- message:
	default:
		logger.Warn("message router buffer is full. dropping message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan spectypes.SSVMessage {
	return r.ch
}
