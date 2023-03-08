package validator

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/forks"
	"go.uber.org/zap"
)

const bufSize = 1024

func newMessageRouter(msgID forks.MsgIDFunc) *messageRouter {
	return &messageRouter{
		ch:    make(chan spectypes.SSVMessage, bufSize),
		msgID: msgID,
	}
}

type messageRouter struct {
	ch    chan spectypes.SSVMessage
	msgID forks.MsgIDFunc
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
