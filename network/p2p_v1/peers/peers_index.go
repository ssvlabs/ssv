package peers

import (
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

// peersIndex implements Index interface
type peersIndex struct {
	logger *zap.Logger

	network libp2pnetwork.Network

	statesLock *sync.RWMutex
	states     map[string]nodeStateObj

	self *Identity

	maxPeers func() int
	pruneTTL time.Duration
}

// NewPeersIndex creates a new Index
func NewPeersIndex(logger *zap.Logger, network libp2pnetwork.Network, self *Identity, maxPeers func() int, pruneTTL time.Duration) Index {
	return &peersIndex{
		logger:     logger,
		network:    network,
		statesLock: &sync.RWMutex{},
		states:     make(map[string]nodeStateObj),
		self:       self,
		maxPeers:   maxPeers,
		pruneTTL:   pruneTTL,
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

func (pi *peersIndex) Limit(dir libp2pnetwork.Direction) bool {
	maxPeers := pi.maxPeers()
	if dir == libp2pnetwork.DirInbound {
		// accepting more connection than the limit for inbound connections
		maxPeers *= 2
	}
	peers := pi.network.Peers()
	return len(peers) < maxPeers
}

func (pi *peersIndex) Self() *Identity {
	return pi.self
}

// Add adds a new peer identity
func (pi *peersIndex) Add(identity *Identity) (bool, error) {
	switch pi.state(identity.ID) {
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
	return pi.add(identity)
}

// Identity returns the identity of the given peer
func (pi *peersIndex) Identity(id peer.ID) (*Identity, error) {
	switch pi.state(id.String()) {
	case StateIndexing:
		// TODO: handle
		return nil, ErrIndexingInProcess
	case StatePruned:
		return nil, ErrWasPruned
	case StateUnknown:
		return nil, ErrNotFound
	default:
	}
	// if in good state -> get identity
	return pi.get(id)
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
	pi.states = nil
	pi.statesLock.Unlock()
	return nil
}

// add saves the given identity
func (pi *peersIndex) add(identity *Identity) (bool, error) {
	pi.setState(identity.ID, StateIndexing)
	pid, err := peer.Decode(identity.ID)
	if err != nil {
		return false, errors.Wrap(err, "could not convert string to peer.ID")
	}
	tx := newTransactional(pid, pi.network.Peerstore())
	tx.Put(formatIdentityKey(forkVKey), identity.ForkV)
	tx.Put(formatIdentityKey(operatorIDKey), identity.OperatorID)
	tx.Put(formatIdentityKey(consensusNodeKey), identity.ConsensusNode())
	tx.Put(formatIdentityKey(execNodeKey), identity.ExecutionNode())
	tx.Put(formatIdentityKey(nodeVersionKey), identity.NodeVersion())
	// commit changes or rollback
	if err := tx.Commit(); err != nil {
		return false, tx.Rollback()
	}
	pi.setState(identity.ID, StateReady)
	return true, nil
}

// add saves the given identity
func (pi *peersIndex) get(pid peer.ID) (*Identity, error) {
	tx := newTransactional(pid, pi.network.Peerstore())
	// build identity object
	fraw, err := tx.Get(formatIdentityKey(forkVKey))
	if err != nil {
		return nil, err
	}
	forkV, ok := fraw.(string)
	if !ok {
		return nil, errors.New("could not cast fork version")
	}
	oraw, err := tx.Get(formatIdentityKey(operatorIDKey))
	if err != nil {
		return nil, err
	}
	oid, ok := oraw.(string)
	if !ok {
		return nil, errors.New("could not cast operator ID")
	}

	metadata, err := pi.getMetadata(tx)

	return NewIdentity(pid.String(), oid, forkV, metadata), err
}

func (pi *peersIndex) getMetadata(tx Transactional) (map[string]string, error) {
	var ok bool
	metadata := make(map[string]string)
	craw, err := tx.Get(formatIdentityKey(consensusNodeKey))
	if err != nil {
		return nil, err
	}
	metadata[consensusNodeKey], ok = craw.(string)
	if !ok {
		return nil, errors.New("could not cast consensus node")
	}
	eraw, err := tx.Get(formatIdentityKey(execNodeKey))
	if err != nil {
		return nil, err
	}
	metadata[execNodeKey], ok = eraw.(string)
	if !ok {
		return nil, errors.New("could not cast exec node")
	}
	vraw, err := tx.Get(formatIdentityKey(nodeVersionKey))
	if err != nil {
		return nil, err
	}
	metadata[nodeVersionKey], ok = vraw.(string)
	if !ok {
		return nil, errors.New("could not cast node version")
	}
	return metadata, nil
}

func (pi *peersIndex) state(pid string) NodeState {
	pi.statesLock.RLock()
	defer pi.statesLock.RUnlock()

	so, ok := pi.states[pid]
	if !ok {
		return StateUnknown
	}
	return so.state
}

func (pi *peersIndex) pruned(pid string) bool {
	pi.statesLock.RLock()
	defer pi.statesLock.RUnlock()

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

func (pi *peersIndex) setState(pid string, state NodeState) {
	pi.statesLock.Lock()
	defer pi.statesLock.Unlock()

	so := nodeStateObj{
		state: state,
		time:  time.Now(),
	}
	pi.states[pid] = so
}
