package mock

import (
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	ma "github.com/multiformats/go-multiaddr"
)

var _ identify.IDService = IDService{}

type IDService struct {
	MockIdentifyWait chan struct{}
}

func (m IDService) IdentifyConn(conn libp2pnetwork.Conn) {
	//TODO implement me
	panic("implement me")
}

func (m IDService) IdentifyWait(conn libp2pnetwork.Conn) <-chan struct{} {
	return m.MockIdentifyWait
}

func (m IDService) OwnObservedAddrs() []ma.Multiaddr {
	//TODO implement me
	panic("implement me")
}

func (m IDService) ObservedAddrsFor(local ma.Multiaddr) []ma.Multiaddr {
	//TODO implement me
	panic("implement me")
}

func (m IDService) Close() error {
	//TODO implement me
	panic("implement me")
}

func (m IDService) Start() {
	//TODO implement me
	panic("implement me")
}
