package mock

import (
	"github.com/libp2p/go-libp2p/core"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/streams"
)

var _ streams.StreamController = StreamController{}

type StreamController struct {
	MockRequest []byte
}

func (m StreamController) Request(logger *zap.Logger, peerID peer.ID, protocol protocol.ID, msg []byte) ([]byte, error) {
	if len(m.MockRequest) != 0 {
		return m.MockRequest, nil
	} else {
		return nil, errors.New("error")
	}
}

func (m StreamController) HandleStream(logger *zap.Logger, stream core.Stream) ([]byte, streams.StreamResponder, func(), error) {
	//TODO implement me
	panic("implement me")
}
