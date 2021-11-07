package p2p

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
)

// ReceivedChannel returns the received-msg-channel for a specific validator
func (n *p2pNetwork) ReceivedChannel(validatorPk *bls.PublicKey) (<-chan *proto.SignedMessage, pubsub.DeregisterFunc) {
	cn, done := n.emitter.Channel(validatorChannel(validatorPk.SerializeToHexStr()))

	cnMsg := make(chan *proto.SignedMessage, 25)
	go func() {
		defer n.logger.Debug("done receiving messages for validator",
			zap.String("pk", validatorPk.SerializeToHexStr()))
		for c := range cn {
			if wm, ok := c.(wireMsg); ok {
				cnMsg <- wm.msg
			}
		}
	}()

	return cnMsg, done
}

type wireMsg struct {
	msg *proto.SignedMessage
}

// Copy copies the object
func (wm wireMsg) Copy() interface{} {
	return wireMsg{msg: wm.msg}
}

func validatorChannel(pk string) string {
	return fmt.Sprintf("in:%s", pk)
}
