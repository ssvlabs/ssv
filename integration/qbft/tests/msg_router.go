package tests

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	protocolvalidator "github.com/bloxapp/ssv/protocol/ssv/validator"
)

type msgRouter struct {
	validator *protocolvalidator.Validator
}

func (m *msgRouter) Route(logger *zap.Logger, message spectypes.SSVMessage) {
	m.validator.HandleMessage(logger, &message)
}

func newMsgRouter(v *protocolvalidator.Validator) *msgRouter {
	return &msgRouter{
		validator: v,
	}
}
