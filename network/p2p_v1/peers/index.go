package peers

import (
	"fmt"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"io"
	"time"
)

const (
	// forkVKey is the key used to store the fork version
	forkVKey = "forkv"
	// operatorIDKey is the key used to store the operatorID
	operatorIDKey = "oid"
	// subnetsKey is the key used to store the subnets list
	subnetsKey = "oid"
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

// NodeState is the state of the node w.r.t to the Index
type NodeState int32

var (
	// StatePruned is the state for pruned nodes
	StatePruned NodeState = -1
	// StateUnknown is the state for unknown peers
	StateUnknown NodeState = 0
	// StateExpired is the state for nodes that their records were expired
	StateExpired NodeState = 1
	// StateIndexing is the state for nodes that are currently being indexed / pending
	StateIndexing NodeState = 2
	// StateReady is the state for a connected, identified node
	StateReady NodeState = 10
)

// nodeStateObj is a wrapper object for a state, has a time for TTL check
type nodeStateObj struct {
	state NodeState
	time  time.Time
}

// ConnectionIndex is an interface for accessing peers connections
type ConnectionIndex interface {
	//// Connectedness returns the connection state of the given peer
	//Connectedness(id peer.ID) libp2pnetwork.Connectedness

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
	Score(id peer.ID, scores ...NodeScore) error
	// GetScore returns the desired score for the given peer
	GetScore(id peer.ID, names ...string) ([]NodeScore, error)
}

// IdentityIndex is an interface for managing peers identity
type IdentityIndex interface {
	// Self returns the current node identity
	Self() *NodeInfo
	// Add indexes the given peer identity
	Add(node *NodeInfo) (bool, error)
	// NodeInfo returns the identity of the given peer
	NodeInfo(id peer.ID) (*NodeInfo, error)
	// State returns the state of the peer in the identity store
	State(id peer.ID) NodeState
	// EvictPruned removes the given operator or peer from pruned list
	EvictPruned(id peer.ID)
	// Prune marks the given peer as pruned
	Prune(id peer.ID) error
	// GC does garbage collection on current peers and states
	GC()
}

// Index is an interface for storing and accessing peers data
// It uses libp2p's Peerstore (github.com/libp2p/go-libp2p-peerstore) to store metadata of peers.
type Index interface {
	ConnectionIndex
	IdentityIndex
	ScoreIndex
	io.Closer
}

func formatIdentityKey(k string) string {
	return fmt.Sprintf("ssv/identity/%s", k)
}

func formatScoreKey(k string) string {
	return fmt.Sprintf("ssv/score/%s", k)
}
