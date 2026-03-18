package p2pv1

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	p2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/network/topics"
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
	return append([]peer.ID(nil), c.peersByTopic[topicName]...), nil
}

func (c *testTopicsController) Topics() []string {
	return append([]string(nil), c.topics...)
}

func (c *testTopicsController) Broadcast(string, []byte, time.Duration) error {
	return nil
}

func (c *testTopicsController) UpdateScoreParams() error {
	return nil
}

func (c *testTopicsController) Close() error {
	return nil
}

var _ topics.Controller = (*testTopicsController)(nil)

type testPeerIndex struct {
	subnets peers.SubnetsIndex
}

func newTestPeerIndex() *testPeerIndex {
	return &testPeerIndex{
		subnets: peers.NewSubnetsIndex(),
	}
}

func (i *testPeerIndex) Connectedness(peer.ID) p2pnet.Connectedness {
	return p2pnet.NotConnected
}

func (i *testPeerIndex) CanConnect(peer.ID) error {
	return nil
}

func (i *testPeerIndex) AtLimit(p2pnet.Direction) bool {
	return false
}

func (i *testPeerIndex) IsBad(peer.ID) bool {
	return false
}

func (i *testPeerIndex) Score(peer.ID, ...*peers.NodeScore) error {
	return nil
}

func (i *testPeerIndex) GetScore(peer.ID, ...string) ([]peers.NodeScore, error) {
	return nil, nil
}

func (i *testPeerIndex) SelfSealed() ([]byte, error) {
	return nil, nil
}

func (i *testPeerIndex) Self() *records.NodeInfo {
	return nil
}

func (i *testPeerIndex) UpdateSelfRecord(func(*records.NodeInfo) *records.NodeInfo) {
}

func (i *testPeerIndex) SetNodeInfo(peer.ID, *records.NodeInfo) {
}

func (i *testPeerIndex) NodeInfo(peer.ID) *records.NodeInfo {
	return nil
}

func (i *testPeerIndex) PeerInfo(peer.ID) *peers.PeerInfo {
	return nil
}

func (i *testPeerIndex) AddPeerInfo(peer.ID, ma.Multiaddr, p2pnet.Direction) {
}

func (i *testPeerIndex) UpdatePeerInfo(peer.ID, func(*peers.PeerInfo)) {
}

func (i *testPeerIndex) State(peer.ID) peers.PeerState {
	return peers.StateUnknown
}

func (i *testPeerIndex) SetState(peer.ID, peers.PeerState) {
}

func (i *testPeerIndex) UpdatePeerSubnets(id peer.ID, subnets commons.Subnets) bool {
	return i.subnets.UpdatePeerSubnets(id, subnets)
}

func (i *testPeerIndex) GetSubnetPeers(subnet int) []peer.ID {
	return i.subnets.GetSubnetPeers(subnet)
}

func (i *testPeerIndex) GetPeerSubnets(id peer.ID) (commons.Subnets, bool) {
	return i.subnets.GetPeerSubnets(id)
}

func (i *testPeerIndex) GetSubnetsStats() *peers.SubnetsStats {
	return i.subnets.GetSubnetsStats()
}

func (i *testPeerIndex) SetScores(map[peer.ID]float64) {
}

func (i *testPeerIndex) GetGossipScore(peer.ID) (float64, bool) {
	return 0, false
}

func (i *testPeerIndex) HasBadGossipScore(peer.ID) (bool, float64) {
	return false, 0
}

func (i *testPeerIndex) Close() error {
	return nil
}

var _ peers.Index = (*testPeerIndex)(nil)

func TestPeerScore_CharacterizesDeadSoloDuoScoring(t *testing.T) {
	candidate := peer.ID("candidate")
	other1 := peer.ID("other-1")
	other2 := peer.ID("other-2")
	other3 := peer.ID("other-3")

	idx := newTestPeerIndex()
	require.True(t, idx.UpdatePeerSubnets(candidate, subnetsFor(0, 1, 2, 3)))

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
		idx,
		subnetsFor(0, 1, 2, 3),
	)

	assert.Equal(t, 21.0, network.peerScore(candidate))
}

func TestPeerScore_ExcludesCandidateFromSubnetCounts(t *testing.T) {
	candidate := peer.ID("candidate")
	other := peer.ID("other")

	idx := newTestPeerIndex()
	require.True(t, idx.UpdatePeerSubnets(candidate, subnetsFor(1)))

	network := newTrimTestNetwork(
		nil,
		&testTopicsController{
			topics: []string{"1"},
			peersByTopic: map[string][]peer.ID{
				"1": {candidate, other},
			},
		},
		idx,
		subnetsFor(1),
	)

	assert.Equal(t, 4.0, network.peerScore(candidate))
}

func TestPeerScore_IgnoresInvalidTopics(t *testing.T) {
	candidate := peer.ID("candidate")
	other := peer.ID("other")

	idx := newTestPeerIndex()
	require.True(t, idx.UpdatePeerSubnets(candidate, subnetsFor(0)))

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
		idx,
		subnetsFor(0),
	)

	assert.Equal(t, 4.0, network.peerScore(candidate))
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

	idx := newTestPeerIndex()
	require.True(t, idx.UpdatePeerSubnets(highValuePeer.ID(), subnetsFor(0)))
	require.True(t, idx.UpdatePeerSubnets(mediumValuePeer.ID(), subnetsFor(1)))
	require.True(t, idx.UpdatePeerSubnets(lowValuePeer.ID(), subnetsFor(3)))

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
		idx,
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

	idx := newTestPeerIndex()
	require.True(t, idx.UpdatePeerSubnets(outboundPeer.ID(), subnetsFor(3)))
	require.True(t, idx.UpdatePeerSubnets(inboundPeer.ID(), subnetsFor(1)))
	require.True(t, idx.UpdatePeerSubnets(highValueInboundPeer.ID(), subnetsFor(0)))

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
		idx,
		subnetsFor(0, 1),
	)

	trimmed := network.choosePeersToTrim(1, true)
	assert.Equal(t, map[peer.ID]struct{}{inboundPeer.ID(): {}}, trimmed)
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
	idx := newTestPeerIndex()
	for i := 0; i < peerCount; i++ {
		remoteHost := newBenchmarkHost(b)
		connectHostsForBenchmark(b, ctx, localHost, remoteHost)

		peerID := remoteHost.ID()
		subnet := uint64(i % topicCount)
		require.True(b, idx.UpdatePeerSubnets(peerID, subnetsFor(subnet)))
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
		idx,
		subnetsFor(0, 1, 2, 3, 4, 5, 6, 7),
	)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = network.choosePeersToTrim(trimCount, false)
	}
}

func newTrimTestNetwork(host host.Host, topicsCtrl topics.Controller, idx peers.Index, ownSubnets commons.Subnets) *p2pNetwork {
	return &p2pNetwork{
		logger:                  zap.NewNop(),
		host:                    host,
		topicsCtrl:              topicsCtrl,
		idx:                     idx,
		persistentSubnets:       ownSubnets,
		subscribedCommittees:    hashmap.New[string, committeeSubscriptionStatus](),
		operatorPKHashToPKCache: hashmap.New[string, []byte](),
	}
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
