package peers

import (
	"fmt"
	"sync"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/records"
)

// MaxPeersProvider returns the max peers for the given topic.
// empty string means that we want to check the total max peers (for all topics).
type MaxPeersProvider func(topic string) int

// NetworkKeyProvider is a function that provides the network private key
type NetworkKeyProvider func() libp2pcrypto.PrivKey

// peersIndex implements Index interface.
type peersIndex struct {
	netKeyProvider NetworkKeyProvider
	network        libp2pnetwork.Network

	scoreIdx ScoreIndex
	SubnetsIndex
	PeerInfoIndex

	selfLock *sync.RWMutex
	self     *records.NodeInfo

	maxPeers MaxPeersProvider

	gossipScoreIndex GossipScoreIndex
}

// NewPeersIndex creates a new Index
func NewPeersIndex(logger *zap.Logger, network libp2pnetwork.Network, self *records.NodeInfo, maxPeers MaxPeersProvider,
	netKeyProvider NetworkKeyProvider, subnetsCount int, pruneTTL time.Duration, gossipScoreIndex GossipScoreIndex) *peersIndex {

	return &peersIndex{
		network:          network,
		scoreIdx:         newScoreIndex(),
		SubnetsIndex:     NewSubnetsIndex(subnetsCount),
		PeerInfoIndex:    NewPeerInfoIndex(),
		self:             self,
		selfLock:         &sync.RWMutex{},
		maxPeers:         maxPeers,
		netKeyProvider:   netKeyProvider,
		gossipScoreIndex: gossipScoreIndex,
	}
}

// IsBad returns whether the given peer is bad.
// a peer is considered to be bad if one of the following applies:
// - bad gossip score
// - pruned (that was not expired)
// - bad score
func (pi *peersIndex) IsBad(logger *zap.Logger, id peer.ID) bool {
	if isBad, _ := pi.HasBadGossipScore(id); isBad {
		return true
	}

	// TODO: check scores
	threshold := -10000.0
	scores, err := pi.GetScore(id, "")
	if err != nil {
		// logger.Debug("could not read score", zap.Error(err))
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

func (pi *peersIndex) CanConnect(id peer.ID) error {
	cntd := pi.network.Connectedness(id)
	switch cntd {
	case libp2pnetwork.Connected:
		return fmt.Errorf("peer already connected")
	default:
	}
	return nil
}

func (pi *peersIndex) AtLimit(dir libp2pnetwork.Direction) bool {
	maxPeers := pi.maxPeers("")
	peers := pi.network.Peers()
	return len(peers) > maxPeers
}

func (pi *peersIndex) UpdateSelfRecord(update func(self *records.NodeInfo) *records.NodeInfo) {
	pi.selfLock.Lock()
	defer pi.selfLock.Unlock()

	pi.self = update(pi.self.Clone())
}

func (pi *peersIndex) Self() *records.NodeInfo {
	pi.selfLock.RLock()
	defer pi.selfLock.RUnlock()

	return pi.self.Clone()
}

func (pi *peersIndex) SelfSealed() ([]byte, error) {
	sealed, err := pi.Self().Seal(pi.netKeyProvider())
	if err != nil {
		return nil, err
	}

	return sealed, nil
}

func (pi *peersIndex) SetNodeInfo(id peer.ID, nodeInfo *records.NodeInfo) {
	pi.UpdatePeerInfo(id, func(info *PeerInfo) {
		info.NodeInfo = nodeInfo
	})
}

func (pi *peersIndex) NodeInfo(id peer.ID) *records.NodeInfo {
	info := pi.PeerInfo(id)
	if info != nil {
		return info.NodeInfo
	}
	return nil
}

// Score adds score to the given peer
func (pi *peersIndex) Score(id peer.ID, scores ...*NodeScore) error {
	return pi.scoreIdx.Score(id, scores...)
}

// GetScore returns the desired score for the given peer
func (pi *peersIndex) GetScore(id peer.ID, names ...string) ([]NodeScore, error) {
	switch pi.State(id) {
	case StateUnknown:
		return nil, ErrNotFound
	}

	return pi.scoreIdx.GetScore(id, names...)
}

func (pi *peersIndex) GetSubnetsStats() *SubnetsStats {
	stats := pi.SubnetsIndex.GetSubnetsStats()
	if stats == nil {
		return nil
	}
	stats.Connected = make([]int, len(stats.PeersCount))
	var sumConnected int
	for subnet := range stats.PeersCount {
		peers := pi.GetSubnetPeers(subnet)
		connectedCount := 0
		for _, p := range peers {
			if pi.Connectedness(p) == libp2pnetwork.Connected {
				connectedCount++
			}
		}
		stats.Connected[subnet] = connectedCount
		sumConnected += connectedCount
	}
	if len(stats.PeersCount) > 0 {
		stats.AvgConnected = sumConnected / len(stats.PeersCount)
	}

	return stats
}

// Close closes peer index
func (pi *peersIndex) Close() error {
	if err := pi.network.Peerstore().Close(); err != nil {
		return errors.Wrap(err, "could not close peerstore")
	}
	return nil
}

// GossipScoreIndex methods
func (pi *peersIndex) SetScores(scores map[peer.ID]float64) {
	pi.gossipScoreIndex.SetScores(scores)
}

func (pi *peersIndex) GetGossipScore(peerID peer.ID) (float64, bool) {
	return pi.gossipScoreIndex.GetGossipScore(peerID)
}

func (pi *peersIndex) HasBadGossipScore(peerID peer.ID) (bool, float64) {
	return pi.gossipScoreIndex.HasBadGossipScore(peerID)
}
