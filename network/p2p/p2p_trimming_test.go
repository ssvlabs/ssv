package p2pv1

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

type testTopicsController struct {
	topics       []string
	peersByTopic map[string][]peer.ID
	allPeers     []peer.ID
}

func (c *testTopicsController) Subscribe(string) error {
	return nil
}

func (c *testTopicsController) Unsubscribe(string, bool) error {
	return nil
}

func (c *testTopicsController) Peers(topicName string) ([]peer.ID, error) {
	if topicName == "" {
		return append([]peer.ID(nil), c.allPeers...), nil
	}
	// The controller presents full topic names outward (see Topics); look up by subnet ID.
	subnet, _, err := commons.ParseTopicSubnet(topicName)
	if err != nil {
		return nil, nil
	}
	return append([]peer.ID(nil), c.peersByTopic[commons.SubnetTopicID(subnet)]...), nil
}

func (c *testTopicsController) Topics() []string {
	// Mirror the real controller, which lists subscribed topics by their full pubsub name.
	// Entries already carrying a full Boole-fork name (e.g. "/ssv/testnet/boole/11") pass
	// through unchanged; bare subnet strings are wrapped in the Alan-style full name.
	// Only those two input shapes are supported: an already-wrapped Alan name (e.g.
	// "ssv.v2.11") has no slash and would be wrapped again, yielding a mangled topic.
	full := make([]string, len(c.topics))
	for i, t := range c.topics {
		if strings.Contains(t, "/") {
			full[i] = t
			continue
		}
		full[i] = commons.GetTopicFullName(t)
	}
	return full
}

func (c *testTopicsController) Broadcast(string, []byte, time.Duration) error {
	return nil
}

func (c *testTopicsController) DeregisterTopics(...string) {}

func (c *testTopicsController) UpdateScoreParams() error {
	return nil
}

func (c *testTopicsController) Close() error {
	return nil
}

var _ topics.Controller = (*testTopicsController)(nil)

func TestBuildPeerTrimScores_CharacterizesDeadSoloDuoScoring(t *testing.T) {
	candidate := peer.ID("candidate")
	other1 := peer.ID("other-1")
	other2 := peer.ID("other-2")
	other3 := peer.ID("other-3")

	network := newTrimTestNetwork(
		nil,
		&testTopicsController{
			topics: []string{"0", "1", "2", "3"},
			peersByTopic: map[string][]peer.ID{
				"0": {candidate},
				"1": {candidate, other1},
				"2": {candidate, other1, other2},
				"3": {candidate, other1, other2, other3},
			},
		},
		nil,
		subnetsFor(0, 1, 2, 3),
	)

	assert.Equal(
		t,
		21.0, // dead + solo + duo
		singleTrimScore(network, candidate),
	)
}

func TestBuildPeerTrimScores_ExcludesCandidateFromSubnetCounts(t *testing.T) {
	candidate := peer.ID("candidate")
	other := peer.ID("other")

	network := newTrimTestNetwork(
		nil,
		&testTopicsController{
			topics: []string{"1"},
			peersByTopic: map[string][]peer.ID{
				"1": {candidate, other},
			},
		},
		nil,
		subnetsFor(1),
	)

	assert.Equal(
		t,
		4.0, // solo
		singleTrimScore(network, candidate),
	)
}

func TestBuildPeerTrimScores_IgnoresInvalidTopics(t *testing.T) {
	candidate := peer.ID("candidate")
	other := peer.ID("other")

	network := newTrimTestNetwork(
		nil,
		&testTopicsController{
			topics: []string{"0", "not-a-subnet", "999"},
			peersByTopic: map[string][]peer.ID{
				"0":            {candidate, other},
				"not-a-subnet": {candidate},
				"999":          {candidate},
			},
		},
		nil,
		subnetsFor(0),
	)

	assert.Equal(
		t,
		4.0, // solo
		singleTrimScore(network, candidate),
	)
}

func TestChoosePeersToTrim_SelectsLowestScorePeers(t *testing.T) {
	ctx := t.Context()
	localHost := newTestHost(t)
	highValuePeer := newTestHost(t)
	mediumValuePeer := newTestHost(t)
	lowValuePeer := newTestHost(t)

	connectHosts(t, ctx, localHost, highValuePeer)
	connectHosts(t, ctx, localHost, mediumValuePeer)
	connectHosts(t, ctx, localHost, lowValuePeer)

	network := newTrimTestNetwork(
		localHost,
		&testTopicsController{
			topics: []string{"0", "1", "3"},
			peersByTopic: map[string][]peer.ID{
				"0": {highValuePeer.ID()},
				"1": {highValuePeer.ID(), mediumValuePeer.ID()},
				"3": {lowValuePeer.ID()},
			},
			allPeers: []peer.ID{highValuePeer.ID(), mediumValuePeer.ID(), lowValuePeer.ID()},
		},
		nil,
		subnetsFor(0, 1),
	)

	trimmed := network.choosePeersToTrim(1, false)
	assert.Equal(t, map[peer.ID]struct{}{lowValuePeer.ID(): {}}, trimmed)
}

func TestChoosePeersToTrim_TrimInboundOnlySkipsOutboundPeers(t *testing.T) {
	ctx := t.Context()
	localHost := newTestHost(t)
	outboundPeer := newTestHost(t)
	inboundPeer := newTestHost(t)
	highValueInboundPeer := newTestHost(t)

	connectHosts(t, ctx, localHost, outboundPeer)
	connectHosts(t, ctx, inboundPeer, localHost)
	connectHosts(t, ctx, highValueInboundPeer, localHost)

	network := newTrimTestNetwork(
		localHost,
		&testTopicsController{
			topics: []string{"0", "1", "3"},
			peersByTopic: map[string][]peer.ID{
				"0": {highValueInboundPeer.ID()},
				"1": {highValueInboundPeer.ID(), inboundPeer.ID()},
				"3": {outboundPeer.ID()},
			},
			allPeers: []peer.ID{outboundPeer.ID(), inboundPeer.ID(), highValueInboundPeer.ID()},
		},
		nil,
		subnetsFor(0, 1),
	)

	trimmed := network.choosePeersToTrim(1, true)
	assert.Equal(t, map[peer.ID]struct{}{inboundPeer.ID(): {}}, trimmed)
}

func TestBuildPeerTrimScores_ComputesScoresForAllCandidates(t *testing.T) {
	highValuePeer := peer.ID("high-value")
	mediumValuePeer := peer.ID("medium-value")
	lowValuePeer := peer.ID("low-value")

	network := newTrimTestNetwork(
		nil,
		&testTopicsController{
			topics: []string{"0", "1", "3"},
			peersByTopic: map[string][]peer.ID{
				"0": {highValuePeer},
				"1": {highValuePeer, mediumValuePeer},
				"3": {lowValuePeer},
			},
		},
		nil,
		subnetsFor(0, 1),
	)

	scores := network.buildPeerTrimScores([]peer.ID{highValuePeer, mediumValuePeer, lowValuePeer})
	assert.Equal(t, map[peer.ID]float64{
		highValuePeer:   16 + 4, // dead + solo
		mediumValuePeer: 4,      // solo
		lowValuePeer:    0,
	}, scores)
}

// TestBuildPeerTrimScores_ComputesScoresForAllCandidates_BooleTopics exercises the same scoring
// logic as TestBuildPeerTrimScores_ComputesScoresForAllCandidates but against a post-fork-pinned
// NetworkConfig and Boole-named topics ("/ssv/<network>/boole/<subnet>"), so the post leg of the
// SSV_TEST_BOOLE_FORK matrix isn't a pure re-run of the pre-fork case.
func TestBuildPeerTrimScores_ComputesScoresForAllCandidates_BooleTopics(t *testing.T) {
	highValuePeer := peer.ID("high-value")
	mediumValuePeer := peer.ID("medium-value")
	lowValuePeer := peer.ID("low-value")

	postForkCfg := trimTestNetCfg(0)
	booleTopic := func(subnet uint64) string {
		return commons.BooleTopic(postForkCfg.SSV.Name, subnet)
	}

	network := newTrimTestNetworkWithConfig(
		postForkCfg,
		nil,
		&testTopicsController{
			topics: []string{booleTopic(0), booleTopic(1), booleTopic(3)},
			peersByTopic: map[string][]peer.ID{
				"0": {highValuePeer},
				"1": {highValuePeer, mediumValuePeer},
				"3": {lowValuePeer},
			},
		},
		nil,
		subnetsFor(0, 1),
	)

	scores := network.buildPeerTrimScores([]peer.ID{highValuePeer, mediumValuePeer, lowValuePeer})
	assert.Equal(t, map[peer.ID]float64{
		highValuePeer:   16 + 4, // dead + solo
		mediumValuePeer: 4,      // solo
		lowValuePeer:    0,
	}, scores)
}

func BenchmarkChoosePeersToTrim_150Peers(b *testing.B) {
	ctx := context.Background()
	localHost := newBenchmarkHost(b)

	const (
		peerCount  = 150
		topicCount = 16
		trimCount  = 4
	)

	topicsByName := make(map[string][]peer.ID, topicCount)
	topicNames := make([]string, 0, topicCount)
	for subnet := range topicCount {
		topicNames = append(topicNames, commons.SubnetTopicID(uint64(subnet)))
	}

	allPeers := make([]peer.ID, 0, peerCount)
	for i := 0; i < peerCount; i++ {
		remoteHost := newBenchmarkHost(b)
		connectHostsForBenchmark(b, ctx, localHost, remoteHost)

		peerID := remoteHost.ID()
		subnet := uint64(i % topicCount)
		topicsByName[commons.SubnetTopicID(subnet)] = append(topicsByName[commons.SubnetTopicID(subnet)], peerID)
		allPeers = append(allPeers, peerID)
	}

	network := newTrimTestNetwork(
		localHost,
		&testTopicsController{
			topics:       topicNames,
			peersByTopic: topicsByName,
			allPeers:     allPeers,
		},
		nil,
		subnetsFor(0, 1, 2, 3, 4, 5, 6, 7),
	)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = network.choosePeersToTrim(trimCount, false)
	}
}

func singleTrimScore(network *p2pNetwork, peerID peer.ID) float64 {
	return network.buildPeerTrimScores([]peer.ID{peerID})[peerID]
}

// trimTestNetCfg returns a per-test NetworkConfig copy (own SSV copy) with Forks.Boole pinned to
// the given epoch, mirroring subscribeRandomsTestNetCfg / TestSubscribedSubnetsForCurrentEpoch's
// pattern elsewhere in this package: never mutate the shared networkconfig.TestNetwork pointer.
func trimTestNetCfg(booleEpoch phase0.Epoch) *networkconfig.Network {
	cfg := *networkconfig.TestNetwork
	beaconCfg := *networkconfig.TestNetwork.Beacon
	ssvCfg := *networkconfig.TestNetwork.SSV
	ssvCfg.Forks.Boole = booleEpoch
	cfg.Beacon = &beaconCfg
	cfg.SSV = &ssvCfg
	return &cfg
}

// newTrimTestNetwork builds a p2pNetwork pinned to a pre-fork NetworkConfig copy, so these tests
// exercise scoring against Alan-shaped topic fixtures regardless of SSV_TEST_BOOLE_FORK: the
// Boole side of the scoring math (SubnetPeers.Score) is already covered fork-independently by
// TestSubnetPeers_Score_BooleTransition and TestSubnetPeers_Score_MixedSubnets in
// p2p_discovery_test.go.
func newTrimTestNetwork(host host.Host, topicsCtrl topics.Controller, idx peers.Index, ownSubnets commons.Subnets) *p2pNetwork {
	return newTrimTestNetworkWithConfig(trimTestNetCfg(phase0.Epoch(math.MaxUint64)), host, topicsCtrl, idx, ownSubnets)
}

func newTrimTestNetworkWithConfig(cfg *networkconfig.Network, host host.Host, topicsCtrl topics.Controller, idx peers.Index, ownSubnets commons.Subnets) *p2pNetwork {
	if idx == nil {
		if host != nil {
			idx = peers.NewPeersIndex(zap.NewNop(), host.Network(), nil, func(string) int { return 0 }, nil, peers.NewGossipScoreIndex())
		} else {
			idx = peers.NewPeersIndex(zap.NewNop(), nil, nil, func(string) int { return 0 }, nil, peers.NewGossipScoreIndex())
		}
		if topicsCtrl != nil {
			for _, topic := range topicsCtrl.Topics() {
				subnet, _, err := commons.ParseTopicSubnet(topic)
				if err != nil || subnet >= commons.SubnetsCount {
					continue
				}
				peersByTopic, err := topicsCtrl.Peers(topic)
				if err != nil {
					continue
				}
				for _, peerID := range peersByTopic {
					subnets, _ := idx.GetPeerSubnets(peerID)
					subnets.Set(subnet)
					idx.UpdatePeerSubnets(peerID, subnets)
				}
			}
		}
	}

	n := &p2pNetwork{
		logger:               zap.NewNop(),
		cfg:                  &Config{NetworkConfig: cfg},
		topicsCtrl:           topicsCtrl,
		idx:                  idx,
		persistentSubnets:    ownSubnets,
		subscribedCommittees: hashmap.New[string, statusWithSubnet](),
	}
	if host != nil {
		n.host.Store(&host)
	}
	return n
}

func subnetsFor(subnets ...uint64) commons.Subnets {
	result := commons.ZeroSubnets
	for _, subnet := range subnets {
		result.Set(subnet)
	}
	return result
}

func newTestHost(t *testing.T) host.Host {
	t.Helper()

	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = h.Close()
	})
	return h
}

func connectHosts(t *testing.T, ctx context.Context, from host.Host, to host.Host) {
	t.Helper()

	require.NoError(t, from.Connect(ctx, peer.AddrInfo{ID: to.ID(), Addrs: to.Addrs()}))
	require.Eventually(t, func() bool {
		return len(from.Network().ConnsToPeer(to.ID())) > 0
	}, 5*time.Second, 10*time.Millisecond)
}

func newBenchmarkHost(b *testing.B) host.Host {
	b.Helper()

	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(b, err)
	b.Cleanup(func() {
		_ = h.Close()
	})
	return h
}

func connectHostsForBenchmark(b *testing.B, ctx context.Context, from host.Host, to host.Host) {
	b.Helper()

	require.NoError(b, from.Connect(ctx, peer.AddrInfo{ID: to.ID(), Addrs: to.Addrs()}))
	require.Eventually(b, func() bool {
		return len(from.Network().ConnsToPeer(to.ID())) > 0
	}, 5*time.Second, 10*time.Millisecond)
}
