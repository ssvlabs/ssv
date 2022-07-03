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

// MaxPeersProvider returns the max peers for the given topic.
// empty string means that we want to check the total max peers (for all topics).
type MaxPeersProvider func(topic string) int

// peersIndex implements Index interface.
// It uses libp2p's Peerstore (github.com/libp2p/go-libp2p-peerstore) to store node info of peers.
type peersIndex struct {
	logger *zap.Logger

	netKeyProvider func() crypto.PrivKey
	network        libp2pnetwork.Network

	states *nodeStates

	selfLock *sync.RWMutex
	self     *records.NodeInfo
	// selfSealed helps to cache the node info record instead of signing multiple times
	selfSealed []byte

	maxPeers func() int
}

// NewPeersIndex creates a new Index
func NewPeersIndex(logger *zap.Logger, network libp2pnetwork.Network, self *records.NodeInfo, maxPeers func() int, netKeyProvider func() crypto.PrivKey, pruneTTL time.Duration) Index {
	return &peersIndex{
		logger:         logger,
		network:        network,
		states:         newNodeStates(pruneTTL),
		self:           self,
		selfLock:       &sync.RWMutex{},
		maxPeers:       maxPeers,
		netKeyProvider: netKeyProvider,
	}
}

// IsBad returns whether the given peer is bad.
// a peer is considered to be bad if one of the following applies:
// - pruned (that was not expired)
// - bad score
func (pi *peersIndex) IsBad(id peer.ID) bool {
	logger := pi.logger.With(zap.String("id", id.String()))
	if pi.states.pruned(id.String()) {
		logger.Debug("bad peer (pruned)")
		return true
	}
	// TODO: check scores
	threshold := -10000.0
	scores, err := pi.GetScore(id, "")
	if err != nil {
		//logger.Debug("could not read score", zap.Error(err))
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
		maxPeers *= 4 / 3
	}
	peers := pi.network.Peers()
	return len(peers) > maxPeers
}

func (pi *peersIndex) UpdateSelfRecord(newSelf *records.NodeInfo) {
	pi.selfLock.Lock()
	defer pi.selfLock.Unlock()

	pi.selfSealed = nil
	pi.self = newSelf
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
	switch pi.states.State(id) {
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
	switch pi.states.State(id) {
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
	return pi.states.State(id)
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
	switch pi.states.State(id) {
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
	return pi.states.Prune(id)
}

// EvictPruned changes to ready state instead of pruned
func (pi *peersIndex) EvictPruned(id peer.ID) {
	pi.states.EvictPruned(id)
}

// GC does garbage collection on current peers and states
func (pi *peersIndex) GC() {
	pi.states.GC()
}

// Close closes peer index
func (pi *peersIndex) Close() error {
	_ = pi.states.Close()
	if err := pi.network.Peerstore().Close(); err != nil {
		return errors.Wrap(err, "could not close peerstore")
	}
	return nil
}

// add saves the given identity
func (pi *peersIndex) add(pid peer.ID, nodeInfo *records.NodeInfo) (bool, error) {
	id := pid.String()
	pi.states.setState(id, StateIndexing)

	raw, err := nodeInfo.MarshalRecord()
	if err != nil {
		pi.states.setState(id, StateUnknown)
		return false, errors.Wrap(err, "could not marshal node info record")
	}
	// commit changes or rollback
	if err := pi.network.Peerstore().Put(pid, formatInfoKey(nodeInfoKey), raw); err != nil {
		pi.states.setState(id, StateUnknown)
		pi.logger.Warn("could not save peer data", zap.Error(err), zap.String("peer", id))
		return false, err
	}
	pi.states.setState(id, StateReady)
	return true, nil
}

// add saves the given identity
func (pi *peersIndex) get(pid peer.ID) (*records.NodeInfo, error) {
	// build identity object
	raw, err := pi.network.Peerstore().Get(pid, formatInfoKey(nodeInfoKey))
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
