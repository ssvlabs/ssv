package peers

import (
	"github.com/libp2p/go-libp2p-core/peer"
	"sync"
	"time"
)

// NodeState is the state of the node w.r.t to the Index
type NodeState int32

func (ns NodeState) String() string {
	switch ns {
	case StatePruned:
		return "pruned"
	case StateIndexing:
		return "indexing"
	case StateReady:
		return "ready"
	default:
		return "unknown"
	}
}

var (
	// StatePruned is the state for pruned nodes
	StatePruned NodeState = -1
	// StateUnknown is the state for unknown peers
	StateUnknown NodeState = 0
	// StateIndexing is the state for nodes that are currently being indexed / pending
	StateIndexing NodeState = 1
	// StateReady is the state for a connected, identified node
	StateReady NodeState = 2
)

// nodeStateObj is a wrapper object for a state, has a time for TTL check
type nodeStateObj struct {
	state NodeState
	time  time.Time
}

// nodeStates implements NodeStates
type nodeStates struct {
	statesLock *sync.RWMutex
	states     map[string]nodeStateObj

	pruneTTL time.Duration
}

// newNodeStates creates an instance of nodeStates
func newNodeStates(pruneTTL time.Duration) *nodeStates {
	return &nodeStates{
		statesLock: &sync.RWMutex{},
		states:     make(map[string]nodeStateObj),
		pruneTTL:   pruneTTL,
	}
}

func (ns *nodeStates) State(id peer.ID) NodeState {
	return ns.state(id.String())
}

func (ns *nodeStates) Prune(id peer.ID) error {
	ns.setState(id.String(), StatePruned)
	return nil
}

func (ns *nodeStates) EvictPruned(id peer.ID) {
	ns.setState(id.String(), StateReady)
}

func (ns *nodeStates) GC() {
	ns.statesLock.Lock()
	defer ns.statesLock.Unlock()

	now := time.Now()
	for pid, s := range ns.states {
		if s.state == StatePruned {
			// check ttl
			if !s.time.Add(ns.pruneTTL).After(now) {
				delete(ns.states, pid)
			}
		}
	}
}

// Close closes node states by initializing the map used to store states
func (ns *nodeStates) Close() error {
	ns.statesLock.Lock()
	defer ns.statesLock.Unlock()
	ns.states = make(map[string]nodeStateObj)
	return nil
}

// state returns the NodeState of the given peer
func (ns *nodeStates) state(pid string) NodeState {
	ns.statesLock.RLock()
	defer ns.statesLock.RUnlock()

	so, ok := ns.states[pid]
	if !ok {
		return StateUnknown
	}
	return so.state
}

// pruned checks if the given peer was pruned and that TTL was not reached
func (ns *nodeStates) pruned(pid string) bool {
	ns.statesLock.Lock()
	defer ns.statesLock.Unlock()

	so, ok := ns.states[pid]
	if !ok {
		return false
	}
	if so.state == StatePruned {
		if so.time.Add(ns.pruneTTL).After(time.Now()) {
			return true
		}
		// TTL reached
		delete(ns.states, pid)
	}
	return false
}

// setState updates the NodeState of the peer
func (ns *nodeStates) setState(pid string, state NodeState) {
	ns.statesLock.Lock()
	defer ns.statesLock.Unlock()

	so := nodeStateObj{
		state: state,
		time:  time.Now(),
	}
	ns.states[pid] = so
}
