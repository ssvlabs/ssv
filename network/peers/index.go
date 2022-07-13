package peers

import (
	"github.com/bloxapp/ssv/network/records"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"io"
)

const (
	// NodeInfoProtocol is the protocol.ID used for handshake
	NodeInfoProtocol = "/ssv/info/0.0.1"
)

var (
	// ErrWasPruned means the desired peer was pruned
	ErrWasPruned = errors.New("peer was pruned")
	// ErrNotFound means the desired peer was not found
	ErrNotFound = errors.New("peer not found")
	// ErrIndexingInProcess means the desired peer is currently being indexed
	ErrIndexingInProcess = errors.New("peer indexing in process")
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
	IsBad(id peer.ID) bool
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
	SelfSealed() ([]byte, error)
	// Self returns the current node info
	Self() *records.NodeInfo
	// UpdateSelfRecord updating current self with new one
	UpdateSelfRecord(newInfo *records.NodeInfo)
	// AddNodeInfo indexes the given peer info
	AddNodeInfo(id peer.ID, node *records.NodeInfo) (bool, error)
	// GetNodeInfo returns the info of the given node
	GetNodeInfo(id peer.ID) (*records.NodeInfo, error)
}

// NodeStates is an interface for managing NodeState across network peers
type NodeStates interface {
	// State returns the state of the peer in the identity store
	State(id peer.ID) NodeState
	// EvictPruned removes the given operator or peer from pruned list
	EvictPruned(id peer.ID)
	// Prune marks the given peer as pruned
	Prune(id peer.ID) error
	// GC does garbage collection on current peers and states
	GC()
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
	NodeStates
	ScoreIndex
	SubnetsIndex
	io.Closer
}
