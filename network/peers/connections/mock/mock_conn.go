//go:build testutils

// This file contains helpers for tests only.
// It will not be compiled into production binaries.

package mock

import (
	"context"

	"github.com/libp2p/go-libp2p/core/crypto"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

var _ libp2pnetwork.Conn = Conn{}

type Conn struct {
	MockPeerID peer.ID
}

func (m Conn) Close() error {
	//TODO implement me
	panic("implement me")
}

func (m Conn) LocalPeer() peer.ID {
	//TODO implement me
	panic("implement me")
}

func (m Conn) LocalPrivateKey() crypto.PrivKey {
	//TODO implement me
	panic("implement me")
}

func (m Conn) RemotePeer() peer.ID {
	return m.MockPeerID
}

func (m Conn) RemotePublicKey() crypto.PubKey {
	//TODO implement me
	panic("implement me")
}

func (m Conn) ConnState() libp2pnetwork.ConnectionState {
	//TODO implement me
	panic("implement me")
}

func (m Conn) LocalMultiaddr() ma.Multiaddr {
	//TODO implement me
	panic("implement me")
}

func (m Conn) RemoteMultiaddr() ma.Multiaddr {
	//TODO implement me
	panic("implement me")
}

func (m Conn) Stat() libp2pnetwork.ConnStats {
	//TODO implement me
	panic("implement me")
}

func (m Conn) Scope() libp2pnetwork.ConnScope {
	//TODO implement me
	panic("implement me")
}

func (m Conn) ID() string {
	//TODO implement me
	panic("implement me")
}

func (m Conn) NewStream(ctx context.Context) (libp2pnetwork.Stream, error) {
	//TODO implement me
	panic("implement me")
}

func (m Conn) GetStreams() []libp2pnetwork.Stream {
	//TODO implement me
	panic("implement me")
}

func (m Conn) IsClosed() bool {
	//TODO implement me
	panic("implement me")
}
