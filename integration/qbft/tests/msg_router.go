package tests

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
)

type msgRouter struct {
	validator *protocolvalidator.Validator
}

func (m *msgRouter) Route(message spectypes.SSVMessage) {
	m.validator.HandleMessage(&message)
}

func newMsgRouter(v *protocolvalidator.Validator) *msgRouter {
	return &msgRouter{
		validator: v,
	}
}
