package mock

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"
)

var _ peerstore.Peerstore = Peerstore{}

type Peerstore struct {
	MockFirstSupportedProtocol string
}

func (p Peerstore) Close() error {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) AddAddr(pid peer.ID, addr ma.Multiaddr, ttl time.Duration) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) AddAddrs(pid peer.ID, addrs []ma.Multiaddr, ttl time.Duration) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) SetAddr(pid peer.ID, addr ma.Multiaddr, ttl time.Duration) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) SetAddrs(pid peer.ID, addrs []ma.Multiaddr, ttl time.Duration) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) UpdateAddrs(pid peer.ID, oldTTL time.Duration, newTTL time.Duration) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) Addrs(pid peer.ID) []ma.Multiaddr {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) AddrStream(ctx context.Context, id peer.ID) <-chan ma.Multiaddr {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) ClearAddrs(pid peer.ID) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) PeersWithAddrs() peer.IDSlice {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) PubKey(id peer.ID) crypto.PubKey {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) AddPubKey(id peer.ID, key crypto.PubKey) error {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) PrivKey(id peer.ID) crypto.PrivKey {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) AddPrivKey(id peer.ID, key crypto.PrivKey) error {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) PeersWithKeys() peer.IDSlice {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) Get(pid peer.ID, key string) (interface{}, error) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) Put(pid peer.ID, key string, val interface{}) error {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) RecordLatency(id peer.ID, duration time.Duration) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) LatencyEWMA(id peer.ID) time.Duration {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) GetProtocols(id peer.ID) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) AddProtocols(id peer.ID, s ...string) error {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) SetProtocols(id peer.ID, s ...string) error {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) RemoveProtocols(id peer.ID, s ...string) error {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) SupportsProtocols(id peer.ID, s ...string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) FirstSupportedProtocol(id peer.ID, s ...string) (string, error) {
	return p.MockFirstSupportedProtocol, nil
}

func (p Peerstore) RemovePeer(id peer.ID) {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) PeerInfo(id peer.ID) peer.AddrInfo {
	//TODO implement me
	panic("implement me")
}

func (p Peerstore) Peers() peer.IDSlice {
	//TODO implement me
	panic("implement me")
}
