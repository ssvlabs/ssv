package controller

import (
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/format"
	"go.uber.org/zap"
)

// MediatorReader is an interface for components that resolving network msg's
type MediatorReader interface {
	GetMsgResolver(networkMsg network.NetworkMsg) func(msg *proto.SignedMessage)
}

// Mediator between network and redirect the proper msg to the proper MediatorReader
type Mediator struct {
	logger *zap.Logger
}

// NewMediator returns new Mediator
func NewMediator(logger *zap.Logger) Mediator {
	return Mediator{
		logger: logger,
	}
}

// Redirect network msg to proper MediatorReader. Also validate the msg itself
func (m *Mediator) Redirect(readerHandler func(publicKey string) (MediatorReader, bool), msg *proto.SignedMessage) {
	if err := auth.BasicMsgValidation().Run(msg); err != nil {
		return
	}
	publicKey, role := format.IdentifierUnformat(string(msg.Message.Lambda)) // TODO need to support multi role types
	logger := m.logger.With(zap.String("publicKey", publicKey), zap.String("role", role))
	if reader, ok := readerHandler(publicKey); ok {
		reader.GetMsgResolver(network.NetworkMsg_IBFTType)(msg)
	} else {
		logger.Warn("failed to find validator reader")
	}
}
