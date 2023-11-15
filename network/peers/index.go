package peers

import (
	"crypto/rsa"
	"io"

	"github.com/libp2p/go-libp2p/core/network"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/records"
)

const (
	// NodeInfoProtocol is the protocol.ID used for handshake
	NodeInfoProtocol = "/ssv/info/0.0.1"
)

var (
	// ErrNotFound means the desired peer was not found
	ErrNotFound = errors.New("peer not found")
)

// NodeScore is a wrapping objet for scores
type NodeScore struct {
	Name  string
	Value float64
}

// ConnectionIndex is an interface for accessing peers connections
type ConnectionIndex interface {
	// Connectedness returns the connection state of the given peer
	Connectedness(id peer.ID) libp2pnetwork.Connectedness

	// CanConnect returns whether we can connect to the given peer,
	// by checking if it is already connected or if we tried to connect to it recently and failed
	CanConnect(id peer.ID) bool

	// Limit checks if the node has reached peers limit
	Limit(dir libp2pnetwork.Direction) bool

	// IsBad returns whether the given peer is bad
	IsBad(logger *zap.Logger, id peer.ID) bool
}

// ScoreIndex is an interface for managing peers scores
type ScoreIndex interface {
	// Score adds score to the given peer
	Score(id peer.ID, scores ...*NodeScore) error
	// GetScore returns the desired score for the given peer
	GetScore(id peer.ID, names ...string) ([]NodeScore, error)
}

// NodeInfoIndex is an interface for managing records.NodeInfo of network peers
type NodeInfoIndex interface {
	// SelfSealed returns a sealed, encoded of self node info
	SelfSealed(sender, recipient peer.ID, permissioned bool, operatorPrivateKey *rsa.PrivateKey) ([]byte, error)

	// Self returns the current node info
	Self() *records.NodeInfo

	// UpdateSelfRecord updating current self with new one
	UpdateSelfRecord(newInfo *records.NodeInfo)

	// SetNodeInfo updates the given peer with the NodeInfo.
	SetNodeInfo(id peer.ID, node *records.NodeInfo)

	// NodeInfo returns the NodeInfo of the given peers, or nil if not found.
	NodeInfo(id peer.ID) *records.NodeInfo
}

// PeerInfoIndex is an interface for managing PeerInfo of network peers
type PeerInfoIndex interface {
	// PeerInfo returns the PeerInfo of the given peer, or nil if not found.
	PeerInfo(peer.ID) *PeerInfo

	// AddPeerInfo adds/updates the record for the given peer.
	AddPeerInfo(id peer.ID, address ma.Multiaddr, direction network.Direction)

	// UpdatePeerInfo calls the given function to update the PeerInfo of the given peer.
	UpdatePeerInfo(id peer.ID, update func(*PeerInfo))

	// State returns the state of the peer.
	State(id peer.ID) PeerState

	// SetState sets the state of the peer.
	SetState(id peer.ID, state PeerState)
}

// SubnetsStats holds a snapshot of subnets stats
type SubnetsStats struct {
	AvgConnected int
	PeersCount   []int
	Connected    []int
}

// SubnetsIndex stores information on subnets.
// it keeps track of subnets but doesn't mind regards actual connections that we have.
type SubnetsIndex interface {
	// UpdatePeerSubnets updates the given peer's subnets
	UpdatePeerSubnets(id peer.ID, s records.Subnets) bool

	// GetSubnetPeers returns peers that are interested in the given subnet
	GetSubnetPeers(s int) []peer.ID

	// GetPeerSubnets returns subnets of the given peer
	GetPeerSubnets(id peer.ID) records.Subnets

	// GetSubnetsStats collects and returns subnets stats
	GetSubnetsStats() *SubnetsStats
}

// Index is a facade interface of this package
type Index interface {
	ConnectionIndex
	NodeInfoIndex
	PeerInfoIndex
	ScoreIndex
	SubnetsIndex
	io.Closer
}
