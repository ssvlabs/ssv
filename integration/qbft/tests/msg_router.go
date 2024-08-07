package tests

import (
	"context"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	protocolvalidator "github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
)

type msgRouter struct {
	logger    *zap.Logger
	validator *protocolvalidator.Validator
}

func (m *msgRouter) Route(_ context.Context, message network.DecodedSSVMessage) {
	m.validator.HandleMessage(m.logger, message.(*queue.SSVMessage))
}

func newMsgRouter(logger *zap.Logger, v *protocolvalidator.Validator) *msgRouter {
	return &msgRouter{
		validator: v,
		logger:    logger,
	}
}
