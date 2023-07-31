package tests

import (
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
)

type msgRouter struct {
	validator *protocolvalidator.Validator
}

func (m *msgRouter) Route(message *queue.DecodedSSVMessage) {
	// TODO: get rid of passing logger to HandleMessage
	m.validator.HandleMessage(zap.L(), message)
}

func newMsgRouter(v *protocolvalidator.Validator) *msgRouter {
	return &msgRouter{
		validator: v,
	}
}
