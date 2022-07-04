package peers

import (
	"github.com/bloxapp/ssv/network/records"
	"github.com/libp2p/go-libp2p-core/crypto"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"strconv"
	"sync"
	"time"
)

const (
	nodeInfoKey = "nodeInfo"
)

// MaxPeersProvider returns the max peers for the given topic.
// empty string means that we want to check the total max peers (for all topics).
type MaxPeersProvider func(topic string) int

// NetworkKeyProvider is a function that provides the network private key
type NetworkKeyProvider func() crypto.PrivKey

// peersIndex implements Index interface.
type peersIndex struct {
	logger *zap.Logger

	netKeyProvider NetworkKeyProvider
	network        libp2pnetwork.Network

	states        *nodeStates
	scoreIdx      ScoreIndex
	subnets       SubnetsIndex
	nodeInfoStore *nodeInfoStore

	selfLock *sync.RWMutex
	self     *records.NodeInfo
	// selfSealed helps to cache the node info record instead of signing multiple times
	selfSealed []byte

	maxPeers MaxPeersProvider
}

// NewPeersIndex creates a new Index
func NewPeersIndex(logger *zap.Logger, network libp2pnetwork.Network, self *records.NodeInfo, maxPeers MaxPeersProvider,
	netKeyProvider NetworkKeyProvider, subnetsCount int, pruneTTL time.Duration) Index {
	return &peersIndex{
		logger:         logger,
		network:        network,
		states:         newNodeStates(pruneTTL),
		scoreIdx:       newScoreIndex(),
		subnets:        newSubnetsIndex(subnetsCount),
		nodeInfoStore:  newNodeInfoStore(logger, network),
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
	maxPeers := pi.maxPeers("")
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

// AddNodeInfo adds a new node info
func (pi *peersIndex) AddNodeInfo(id peer.ID, nodeInfo *records.NodeInfo) (bool, error) {
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
	pid := id.String()
	pi.states.setState(pid, StateIndexing)
	added, err := pi.nodeInfoStore.Add(id, nodeInfo)
	if err != nil || !added {
		pi.states.setState(pid, StateUnknown)
	} else {
		pi.states.setState(pid, StateReady)
	}
	return added, err
}

// GetNodeInfo returns the node info of the given peer
func (pi *peersIndex) GetNodeInfo(id peer.ID) (*records.NodeInfo, error) {
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
	ni, err := pi.nodeInfoStore.Get(id)
	if err == peerstore.ErrNotFound {
		return nil, ErrNotFound
	}

	return ni, err
}

func (pi *peersIndex) State(id peer.ID) NodeState {
	return pi.states.State(id)
}

// Score adds score to the given peer
func (pi *peersIndex) Score(id peer.ID, scores ...*NodeScore) error {
	return pi.scoreIdx.Score(id, scores...)
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

	return pi.scoreIdx.GetScore(id, names...)
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

func (pi *peersIndex) UpdatePeerSubnets(id peer.ID, s records.Subnets) bool {
	return pi.subnets.UpdatePeerSubnets(id, s)
}

func (pi *peersIndex) GetSubnetPeers(subnet int) []peer.ID {
	return pi.subnets.GetSubnetPeers(subnet)
}

func (pi *peersIndex) GetPeerSubnets(id peer.ID) records.Subnets {
	return pi.subnets.GetPeerSubnets(id)
}

func (pi *peersIndex) GetSubnetsStats() *SubnetsStats {
	stats := pi.subnets.GetSubnetsStats()
	if stats == nil {
		return nil
	}
	stats.Connected = make([]int, len(stats.PeersCount))
	for subnet, count := range stats.PeersCount {
		metricsSubnetsKnownPeers.WithLabelValues(strconv.Itoa(subnet)).Add(float64(count))
		peers := pi.subnets.GetSubnetPeers(subnet)
		connectedCount := 0
		for _, p := range peers {
			if pi.Connectedness(p) == libp2pnetwork.Connected {
				connectedCount++
			}
		}
		stats.Connected[subnet] = connectedCount
		metricsSubnetsKnownPeers.WithLabelValues(strconv.Itoa(subnet)).Add(float64(connectedCount))
	}
	return stats
}

// Close closes peer index
func (pi *peersIndex) Close() error {
	_ = pi.states.Close()
	if err := pi.network.Peerstore().Close(); err != nil {
		return errors.Wrap(err, "could not close peerstore")
	}
	return nil
}
