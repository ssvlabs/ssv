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
/* implementation example -
networkMediator: NewMediator(options.Logger),
chan := make(chan int)
secChan := make(chan int)
select {
case msg := <-chan:
	networkMediator.redirect(func(publicKey string) (MediatorReader, bool) {
		return reader.(ibftController.MediatorReader), ok
	}, msg)
case msg := <-secChan:
	networkMediator.redirect(func(publicKey string) (MediatorReader, bool) {
		return reader.(ibftController.MediatorReader), ok
	}, msg)
default:
}
*/
type Mediator struct {
	logger *zap.Logger
}

// NewMediator returns new Mediator
func NewMediator(logger *zap.Logger) Mediator {
	return Mediator{
		logger: logger,
	}
}

// AddListener listen to channel and use redirect func to push to the right place
func (m *Mediator) AddListener(ibftType network.NetworkMsg, ch <-chan *proto.SignedMessage, done func(), handler func(publicKey string) (MediatorReader, bool)) {
	go func() {
		defer done()
		for msg := range ch {
			m.redirect(ibftType, handler, msg)
		}

		m.logger.Debug("mediator stopped listening to network", zap.String("type", ibftType.String()))
	}()
}

// redirect network msg to proper MediatorReader. Also validate the msg itself
func (m *Mediator) redirect(ibftType network.NetworkMsg, readerHandler func(publicKey string) (MediatorReader, bool), msg *proto.SignedMessage) {
	if err := auth.BasicMsgValidation().Run(msg); err != nil {
		return
	}
	publicKey, role := format.IdentifierUnformat(string(msg.Message.Lambda)) // TODO need to support multi role types
	logger := m.logger.With(zap.String("publicKey", publicKey), zap.String("role", role), zap.String("type", ibftType.String()))
	if reader, ok := readerHandler(publicKey); ok {
		logger.Debug("got mediator msg")
		reader.GetMsgResolver(ibftType)(msg)
	} else {
		logger.Warn("failed to find validator reader")
	}
}
