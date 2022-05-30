package peers

import (
	"github.com/bloxapp/ssv/network/records"
	"github.com/libp2p/go-libp2p-core/crypto"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	nodeInfoKey = "nodeInfo"
)

// peersIndex implements Index interface
type peersIndex struct {
	logger *zap.Logger

	netKeyProvider func() crypto.PrivKey
	network        libp2pnetwork.Network

	statesLock *sync.RWMutex
	states     map[string]nodeStateObj

	selfLock *sync.RWMutex
	self     *records.NodeInfo
	// selfSealed helps to cache the node info record instead of signing multiple times
	selfSealed []byte

	maxPeers func() int
	pruneTTL time.Duration
}

// NewPeersIndex creates a new Index
func NewPeersIndex(logger *zap.Logger, network libp2pnetwork.Network, self *records.NodeInfo, maxPeers func() int, netKeyProvider func() crypto.PrivKey, pruneTTL time.Duration) Index {
	return &peersIndex{
		logger:         logger,
		network:        network,
		statesLock:     &sync.RWMutex{},
		states:         make(map[string]nodeStateObj),
		self:           self,
		selfLock:       &sync.RWMutex{},
		maxPeers:       maxPeers,
		pruneTTL:       pruneTTL,
		netKeyProvider: netKeyProvider,
	}
}

// IsBad returns whether the given peer is bad.
// a peer is considered to be bad if one of the following applies:
// - pruned (that was not expired)
// - bad score
func (pi *peersIndex) IsBad(id peer.ID) bool {
	logger := pi.logger.With(zap.String("id", id.String()))
	if pi.pruned(id.String()) {
		logger.Debug("bad peer (pruned)")
		return true
	}
	// TODO: check scores
	threshold := -10000.0
	scores, err := pi.GetScore(id, "")
	if err != nil {
		logger.Warn("could not read score", zap.Error(err))
		return false
	}
	for _, score := range scores {
		if score.Value < threshold {
			logger.Debug("bad peer (low score)")
			return true
		}
	}
	return false
}

func (pi *peersIndex) Connectedness(id peer.ID) libp2pnetwork.Connectedness {
	return pi.network.Connectedness(id)
}

func (pi *peersIndex) CanConnect(id peer.ID) bool {
	cntd := pi.network.Connectedness(id)
	switch cntd {
	case libp2pnetwork.Connected:
		fallthrough
	case libp2pnetwork.CannotConnect: // recently failed to connect
		return false
	default:
	}
	return true
}

func (pi *peersIndex) Limit(dir libp2pnetwork.Direction) bool {
	maxPeers := pi.maxPeers()
	if dir == libp2pnetwork.DirInbound {
		// accepting more connection than the limit for inbound connections
		maxPeers *= 2
	}
	peers := pi.network.Peers()
	return len(peers) > maxPeers
}

func (pi *peersIndex) Self() *records.NodeInfo {
	return pi.self
}

func (pi *peersIndex) SelfSealed() ([]byte, error) {
	pi.selfLock.Lock()
	defer pi.selfLock.Unlock()

	if len(pi.selfSealed) == 0 {
		sealed, err := pi.self.Seal(pi.netKeyProvider())
		if err != nil {
			return nil, err
		}
		pi.selfSealed = sealed
	}

	return pi.selfSealed, nil
}

// Add adds a new peer identity
func (pi *peersIndex) Add(id peer.ID, nodeInfo *records.NodeInfo) (bool, error) {
	switch pi.state(id.String()) {
	case StateReady:
		return true, nil
	case StateIndexing:
		// TODO: handle
		return true, nil
	case StatePruned:
		return false, ErrWasPruned
	case StateUnknown:
	default:
	}
	return pi.add(id, nodeInfo)
}

// NodeInfo returns the identity of the given peer
func (pi *peersIndex) NodeInfo(id peer.ID) (*records.NodeInfo, error) {
	switch pi.state(id.String()) {
	case StateIndexing:
		return nil, ErrIndexingInProcess
	case StatePruned:
		return nil, ErrWasPruned
	case StateUnknown:
		return nil, ErrNotFound
	default:
	}
	// if in good state -> get node info
	ni, err := pi.get(id)
	if err == peerstore.ErrNotFound {
		return nil, ErrNotFound
	}

	return ni, err
}

func (pi *peersIndex) State(id peer.ID) NodeState {
	return pi.state(id.String())
}

// Score adds score to the given peer
func (pi *peersIndex) Score(id peer.ID, scores ...NodeScore) error {
	tx := newTransactional(id, pi.network.Peerstore())
	defer func() {
		_ = tx.Close()
	}()
	for _, score := range scores {
		tx.Put(formatScoreKey(score.Name), score.Value)
	}
	if err := tx.Commit(); err != nil {
		return tx.Rollback()
	}
	return nil
}

// GetScore returns the desired score for the given peer
func (pi *peersIndex) GetScore(id peer.ID, names ...string) ([]NodeScore, error) {
	var scores []NodeScore
	switch pi.state(id.String()) {
	case StateIndexing:
		// TODO: handle
		return scores, nil
	case StatePruned:
		return nil, ErrWasPruned
	case StateUnknown:
		return nil, ErrNotFound
	default:
	}
	tx := newTransactional(id, pi.network.Peerstore())
	defer func() {
		_ = tx.Close()
	}()
	for _, name := range names {
		s, err := tx.Get(formatScoreKey(name))
		if err != nil {
			return nil, err
		}
		score, ok := s.(NodeScore)
		if !ok {
			return nil, errors.New("could not cast node score")
		}
		scores = append(scores, score)
	}
	return scores, nil
}

// Prune set prune state for the given peer
func (pi *peersIndex) Prune(id peer.ID) error {
	pi.setState(id.String(), StatePruned)
	return nil
}

// EvictPruned changes to ready state instead of pruned
func (pi *peersIndex) EvictPruned(id peer.ID) {
	pi.setState(id.String(), StateReady)
}

// GC does garbage collection on current peers and states
func (pi *peersIndex) GC() {
	pi.statesLock.Lock()
	defer pi.statesLock.Unlock()

	now := time.Now()
	for pid, s := range pi.states {
		if s.state == StatePruned {
			// check ttl
			if !s.time.Add(pi.pruneTTL).After(now) {
				delete(pi.states, pid)
			}
		}
	}
}

// Close closes peer index
func (pi *peersIndex) Close() error {
	pi.statesLock.Lock()
	pi.states = make(map[string]nodeStateObj)
	pi.statesLock.Unlock()
	if err := pi.network.Peerstore().Close(); err != nil {
		return errors.Wrap(err, "could not close peerstore")
	}
	return nil
}

// add saves the given identity
func (pi *peersIndex) add(pid peer.ID, nodeInfo *records.NodeInfo) (bool, error) {
	id := pid.String()
	pi.setState(id, StateIndexing)

	raw, err := nodeInfo.MarshalRecord()
	if err != nil {
		pi.setState(id, StateUnknown)
		return false, errors.Wrap(err, "could not marshal node info record")
	}
	tx := newTransactional(pid, pi.network.Peerstore())
	tx.Put(formatInfoKey(nodeInfoKey), raw)
	// commit changes or rollback
	if err := tx.Commit(); err != nil {
		pi.setState(id, StateUnknown)
		pi.logger.Warn("could not save peer data", zap.Error(err), zap.String("peer", id))
		return false, tx.Rollback()
	}
	pi.setState(id, StateReady)
	return true, nil
}

// add saves the given identity
func (pi *peersIndex) get(pid peer.ID) (*records.NodeInfo, error) {
	tx := newTransactional(pid, pi.network.Peerstore())
	// build identity object
	raw, err := tx.Get(formatInfoKey(nodeInfoKey))
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	var ni records.NodeInfo
	err = ni.UnmarshalRecord(raw.([]byte))
	if err != nil {
		return nil, err
	}

	return &ni, nil
}

// state returns the NodeState of the given peer
func (pi *peersIndex) state(pid string) NodeState {
	pi.statesLock.RLock()
	defer pi.statesLock.RUnlock()

	so, ok := pi.states[pid]
	if !ok {
		return StateUnknown
	}
	return so.state
}

// pruned checks if the given peer was pruned and that TTL was not reached
func (pi *peersIndex) pruned(pid string) bool {
	pi.statesLock.Lock()
	defer pi.statesLock.Unlock()

	so, ok := pi.states[pid]
	if !ok {
		return false
	}
	if so.state == StatePruned {
		if so.time.Add(pi.pruneTTL).After(time.Now()) {
			return true
		}
		// TTL reached
		delete(pi.states, pid)
	}
	return false
}

// setState updates the NodeState of the peer
func (pi *peersIndex) setState(pid string, state NodeState) {
	pi.statesLock.Lock()
	defer pi.statesLock.Unlock()

	so := nodeStateObj{
		state: state,
		time:  time.Now(),
	}
	pi.states[pid] = so
}
