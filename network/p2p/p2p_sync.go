package p2pv1

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocol_p2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	libp2p_protocol "github.com/libp2p/go-libp2p-core/protocol"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// LastDecided fetches last decided from a random set of peers
func (n *p2pNetwork) LastDecided(mid message.Identifier) ([]message.SSVMessage, error) {
	if !n.isReady() {
		return nil, ErrNetworkIsNotReady
	}
	// TODO
	return nil, errors.New("not implemented")
}

// GetHistory sync the given range from a set of peers that supports history for the given identifier
func (n *p2pNetwork) GetHistory(mid message.Identifier, from, to uint64) ([]message.SSVMessage, error) {
	if !n.isReady() {
		return nil, ErrNetworkIsNotReady
	}
	// TODO
	return nil, errors.New("not implemented")
}

// LastChangeRound fetches last change round message from a random set of peers
func (n *p2pNetwork) LastChangeRound(mid message.Identifier) ([]message.SSVMessage, error) {
	if !n.isReady() {
		return nil, ErrNetworkIsNotReady
	}
	// TODO
	return nil, errors.New("not implemented")
}

// RegisterHandler registers the given handler for the stream
func (n *p2pNetwork) RegisterHandler(pid string, handler protocol_p2p.RequestHandler) {
	n.host.SetStreamHandler(libp2p_protocol.ID(pid), func(stream libp2pnetwork.Stream) {
		req, respond, done, err := n.streamCtrl.HandleStream(stream)
		defer done()
		if err != nil {
			//n.logger.Warn("could not handle stream", zap.Error(err))
			return
		}
		msg, err := n.fork.DecodeNetworkMsg(req)
		if err != nil {
			n.logger.Warn("could not decode msg from stream", zap.Error(err))
			return
		}
		smsg, ok := msg.(*message.SSVMessage)
		if !ok {
			n.logger.Warn("could not cast msg from stream", zap.Error(err))
			return
		}
		result, err := handler(smsg)
		if err != nil {
			n.logger.Warn("could not handle msg from stream")
			return
		}
		resultBytes, err := n.fork.EncodeNetworkMsg(result)
		if err != nil {
			n.logger.Warn("could not encode msg", zap.Error(err))
			return
		}
		if err := respond(resultBytes); err != nil {
			n.logger.Warn("could not respond to stream", zap.Error(err))
			return
		}
	})
}
