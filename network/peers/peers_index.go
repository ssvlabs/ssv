package peers

import (
	"crypto"
	"crypto/rsa"
	"strconv"
	"sync"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/utils/rsaencryption"
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
}

// NewPeersIndex creates a new Index
func NewPeersIndex(logger *zap.Logger, network libp2pnetwork.Network, self *records.NodeInfo, maxPeers MaxPeersProvider,
	netKeyProvider NetworkKeyProvider, subnetsCount int, pruneTTL time.Duration) *peersIndex {
	return &peersIndex{
		network:        network,
		scoreIdx:       newScoreIndex(),
		SubnetsIndex:   NewSubnetsIndex(subnetsCount),
		PeerInfoIndex:  NewPeerInfoIndex(),
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
func (pi *peersIndex) IsBad(logger *zap.Logger, id peer.ID) bool {
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
	peers := pi.network.Peers()
	return len(peers) > maxPeers
}

func (pi *peersIndex) UpdateSelfRecord(newSelf *records.NodeInfo) {
	pi.selfLock.Lock()
	defer pi.selfLock.Unlock()

	pi.self = newSelf
}

func (pi *peersIndex) Self() *records.NodeInfo {
	return pi.self
}

func (pi *peersIndex) SelfSealed(sender, recipient peer.ID, permissioned bool, operatorPrivateKey *rsa.PrivateKey) ([]byte, error) {
	pi.selfLock.Lock()
	defer pi.selfLock.Unlock()

	if permissioned {
		publicKey, err := rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			return nil, err
		}

		handshakeData := records.HandshakeData{
			SenderPeerID:    sender,
			RecipientPeerID: recipient,
			Timestamp:       time.Now(),
			SenderPublicKey: []byte(publicKey),
		}
		hash := handshakeData.Hash()

		signature, err := rsa.SignPKCS1v15(nil, operatorPrivateKey, crypto.SHA256, hash[:])
		if err != nil {
			return nil, err
		}

		signedNodeInfo := &records.SignedNodeInfo{
			NodeInfo:      pi.self,
			HandshakeData: handshakeData,
			Signature:     signature,
		}

		sealed, err := signedNodeInfo.Seal(pi.netKeyProvider())
		if err != nil {
			return nil, err
		}

		return sealed, nil
	}

	sealed, err := pi.self.Seal(pi.netKeyProvider())
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
	mySubnets, err := records.Subnets{}.FromString(pi.self.Metadata.Subnets)
	if err != nil {
		mySubnets, _ = records.Subnets{}.FromString(records.ZeroSubnets)
	}
	stats := pi.SubnetsIndex.GetSubnetsStats()
	if stats == nil {
		return nil
	}
	stats.Connected = make([]int, len(stats.PeersCount))
	var sumConnected int
	for subnet, count := range stats.PeersCount {
		metricsSubnetsKnownPeers.WithLabelValues(strconv.Itoa(subnet)).Set(float64(count))
		metricsMySubnets.WithLabelValues(strconv.Itoa(subnet)).Set(float64(mySubnets[subnet]))
		peers := pi.SubnetsIndex.GetSubnetPeers(subnet)
		connectedCount := 0
		for _, p := range peers {
			if pi.Connectedness(p) == libp2pnetwork.Connected {
				connectedCount++
			}
		}
		stats.Connected[subnet] = connectedCount
		sumConnected += connectedCount
		metricsSubnetsConnectedPeers.WithLabelValues(strconv.Itoa(subnet)).Set(float64(connectedCount))
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
