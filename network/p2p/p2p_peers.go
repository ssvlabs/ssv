package p2pv1

import (
	"bytes"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/records"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
	"sort"
)

// UpdateSubnets will update the registered subnets according to active validators
// NOTE: it won't subscribe to the subnets (use subscribeToSubnets for that)
func (n *p2pNetwork) UpdateSubnets() {
	visited := make(map[int]bool)
	n.activeValidatorsLock.Lock()
	last := make([]byte, len(n.subnets))
	if len(n.subnets) > 0 {
		copy(last, n.subnets)
	}
	newSubnets := make([]byte, n.fork.Subnets())
	for pkHex, state := range n.activeValidators {
		if state == validatorStateInactive {
			continue
		}
		subnet := n.fork.ValidatorSubnet(pkHex)
		if _, ok := visited[subnet]; ok {
			continue
		}
		newSubnets[subnet] = byte(1)
	}
	subnetsToAdd := make([]int, 0)
	if !bytes.Equal(newSubnets, last) { // have changes
		n.subnets = newSubnets
		for i, b := range newSubnets {
			if b == byte(1) {
				subnetsToAdd = append(subnetsToAdd, i)
			}
		}
	}
	n.activeValidatorsLock.Unlock()

	if len(subnetsToAdd) == 0 {
		n.logger.Debug("no changes in subnets")
		return
	}

	self := n.idx.Self()
	self.Metadata.Subnets = records.Subnets(n.subnets).String()
	n.idx.UpdateSelfRecord(self)

	allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
	subnetsList := records.SharedSubnets(allSubs, n.subnets, 0)
	n.logger.Debug("updated subnets (node-info)", zap.Any("subnets", subnetsList))

	err := n.disc.RegisterSubnets(subnetsToAdd...)
	if err != nil {
		n.logger.Warn("could not register subnets", zap.Error(err))
		return
	}
	n.logger.Debug("updated subnets (discovery)", zap.Any("subnets", n.subnets))
}

// getMaxPeers returns max peers of the given topic.
func (n *p2pNetwork) getMaxPeers(topic string) int {
	if len(topic) == 0 {
		return n.cfg.MaxPeers
	}
	baseName := n.fork.GetTopicBaseName(topic)
	if baseName == n.fork.DecidedTopic() { // allow more peers for decided topic
		return n.cfg.TopicMaxPeers * 2
	}
	return n.cfg.TopicMaxPeers
}

func (n *p2pNetwork) tagBestPeers(count int) {
	allPeers := n.host.Network().Peers()
	bestPeers := n.getBestPeers(count, allPeers)
	if len(bestPeers) == 0 {
		return
	}
	n.logger.Debug("found best peers",
		zap.Int("allPeersCount", len(allPeers)),
		zap.Int("bestPeersCount", len(bestPeers)),
		zap.Any("bestPeers", bestPeers))
	for _, pid := range allPeers {
		if _, ok := bestPeers[pid]; ok {
			n.connManager.Protect(pid, "ssv/subnets")
			continue
		}
		n.connManager.Unprotect(pid, "ssv/subnets")
	}
}

// getBestPeers loop over all the existing peers and returns the best set
// according to the number of shared subnets,
// while considering subnets with low peer count to be more important.
// it enables to distribute peers connections across subnets in a balanced way.
func (n *p2pNetwork) getBestPeers(count int, allPeers []peer.ID) map[peer.ID]int {
	if len(allPeers) < count {
		return nil
	}
	stats := n.idx.GetSubnetsStats()
	subnetsScores := n.getSubnetsDistributionScores(stats, allPeers)

	peerScores := make(map[peer.ID]int)
	for _, pid := range allPeers {
		var peerScore int
		subnets := n.idx.GetPeerSubnets(pid)
		for subnet, val := range subnets {
			if val == byte(0) && subnetsScores[subnet] < 0 {
				peerScore -= subnetsScores[subnet]
			} else {
				peerScore += subnetsScores[subnet]
			}
		}
		// adding the number of shared subnets to the score, considering only up to 25% subnets
		shared := records.SharedSubnets(subnets, n.subnets, n.fork.Subnets()/4)
		peerScore += len(shared) / 2
		n.logger.Debug("peer score", zap.String("id", pid.String()), zap.Int("score", peerScores[pid]))
		peerScores[pid] = peerScore
	}

	return getTopScores(peerScores, count)
}

// getSubnetsDistributionScores
func (n *p2pNetwork) getSubnetsDistributionScores(stats *peers.SubnetsStats, allPeers []peer.ID) []int {
	peersCount := len(allPeers)
	minPerSubnet := peersCount / 10
	allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
	activeSubnets := records.SharedSubnets(allSubs, n.subnets, 0)

	scores := make([]int, len(allSubs))
	for _, s := range activeSubnets {
		var connected int
		if s < len(stats.Connected) {
			connected = stats.Connected[s]
		}
		if connected == 0 {
			scores[s] = 2
		} else if connected <= minPerSubnet {
			scores[s] = 1
		} else if connected >= n.cfg.TopicMaxPeers {
			scores[s] = -1
		}
	}
	return scores
}

func getTopScores(peerScores map[peer.ID]int, count int) map[peer.ID]int {
	pl := make(peerScoresList, len(peerScores))
	i := 0
	for k, v := range peerScores {
		pl[i] = peerScorePair{k, v}
		i++
	}
	sort.Sort(sort.Reverse(pl))
	res := make(map[peer.ID]int)
	for _, item := range pl {
		res[item.Key] = item.Value
		if len(res) >= count {
			break
		}
	}
	return res
}

type peerScorePair struct {
	Key   peer.ID
	Value int
}

type peerScoresList []peerScorePair

func (p peerScoresList) Len() int           { return len(p) }
func (p peerScoresList) Less(i, j int) bool { return p[i].Value < p[j].Value }
func (p peerScoresList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
