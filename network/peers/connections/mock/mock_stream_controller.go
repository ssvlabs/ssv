package mock

import (
	"github.com/bloxapp/ssv/network/streams"
	"github.com/libp2p/go-libp2p/core"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"go.uber.org/zap"
)

var _ streams.StreamController = StreamController{}

type StreamController struct {
	MockRequest []byte
}

func (m StreamController) Request(logger *zap.Logger, peerID peer.ID, protocol protocol.ID, msg []byte) ([]byte, error) {
	return m.MockRequest, nil
}

func (m StreamController) HandleStream(logger *zap.Logger, stream core.Stream) ([]byte, streams.StreamResponder, func(), error) {
	//TODO implement me
	panic("implement me")
}
