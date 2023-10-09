package tests

import (
	"context"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
)

type msgRouter struct {
	logger    *zap.Logger
	validator *protocolvalidator.Validator
}

func (m *msgRouter) Route(_ context.Context, message *queue.DecodedSSVMessage) {
	m.validator.HandleMessage(m.logger, message)
}

func newMsgRouter(logger *zap.Logger, v *protocolvalidator.Validator) *msgRouter {
	return &msgRouter{
		validator: v,
		logger:    logger,
	}
}
