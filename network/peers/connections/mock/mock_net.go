//go:build testutils

// This file contains helpers for tests only.
// It will not be compiled into production binaries.

package mock

import (
	"context"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"
)

var _ libp2pnetwork.Network = Net{}

type Net struct {
	MockPeerstore peerstore.Peerstore
}

func (m Net) Peerstore() peerstore.Peerstore {
	return m.MockPeerstore
}

func (m Net) LocalPeer() peer.ID {
	return "some peer"
}

func (m Net) DialPeer(ctx context.Context, id peer.ID) (libp2pnetwork.Conn, error) {
	//TODO implement me
	panic("implement me")
}

func (m Net) ClosePeer(id peer.ID) error {
	//TODO implement me
	panic("implement me")
}

func (m Net) Connectedness(id peer.ID) libp2pnetwork.Connectedness {
	//TODO implement me
	panic("implement me")
}

func (m Net) Peers() []peer.ID {
	//TODO implement me
	panic("implement me")
}

func (m Net) Conns() []libp2pnetwork.Conn {
	//TODO implement me
	panic("implement me")
}

func (m Net) ConnsToPeer(p peer.ID) []libp2pnetwork.Conn {
	//TODO implement me
	panic("implement me")
}

func (m Net) Notify(notifiee libp2pnetwork.Notifiee) {
	//TODO implement me
	panic("implement me")
}

func (m Net) StopNotify(notifiee libp2pnetwork.Notifiee) {
	//TODO implement me
	panic("implement me")
}

func (m Net) CanDial(p peer.ID, addr ma.Multiaddr) bool {
	//TODO implement me
	panic("implement me")
}

func (m Net) Close() error {
	//TODO implement me
	panic("implement me")
}

func (m Net) SetStreamHandler(handler libp2pnetwork.StreamHandler) {
	//TODO implement me
	panic("implement me")
}

func (m Net) NewStream(ctx context.Context, id peer.ID) (libp2pnetwork.Stream, error) {
	//TODO implement me
	panic("implement me")
}

func (m Net) Listen(multiaddr ...ma.Multiaddr) error {
	//TODO implement me
	panic("implement me")
}

func (m Net) ListenAddresses() []ma.Multiaddr {
	//TODO implement me
	panic("implement me")
}

func (m Net) InterfaceListenAddresses() ([]ma.Multiaddr, error) {
	//TODO implement me
	panic("implement me")
}

func (m Net) ResourceManager() libp2pnetwork.ResourceManager {
	//TODO implement me
	panic("implement me")
}
