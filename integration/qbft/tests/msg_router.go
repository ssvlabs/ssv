package tests

import (
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
)

type msgRouter struct {
	validator *protocolvalidator.Validator
}

func (m *msgRouter) Route(logger *zap.Logger, message *queue.DecodedSSVMessage) {
	m.validator.HandleMessage(logger, message)
}

func newMsgRouter(v *protocolvalidator.Validator) *msgRouter {
	return &msgRouter{
		validator: v,
	}
}
