package peers

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/records"
	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTagBestPeers(t *testing.T) {
	logger := logging.TestLogger(t)
	connMgrMock := newConnMgr()

	allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
	si := newSubnetsIndex(len(allSubs))

	cm := NewConnManager(zap.NewNop(), connMgrMock, si).(*connManager)

	pids, err := createPeerIDs(50)
	require.NoError(t, err)

	for _, pid := range pids {
		r := rand.Intn(len(allSubs) / 3)
		si.UpdatePeerSubnets(pid, createRandomSubnets(r))
	}
	mySubnets := createRandomSubnets(40)

	best := cm.getBestPeers(40, mySubnets, pids, 10)
	require.Len(t, best, 40)

	cm.TagBestPeers(logger, 20, mySubnets, pids, 10)
	require.Equal(t, 20, len(connMgrMock.tags))
}

func createRandomSubnets(n int) records.Subnets {
	subnets, _ := records.Subnets{}.FromString(records.ZeroSubnets)
	size := len(subnets)
	for n > 0 {
		i := rand.Intn(size)
		for subnets[i] == byte(1) {
			i = rand.Intn(size)
		}
		subnets[i] = byte(1)
		n--
	}
	return subnets
}

type mockConnManager struct {
	tags map[peer.ID]string
}

var _ connmgrcore.ConnManager = (*mockConnManager)(nil)

func newConnMgr() *mockConnManager {
	return &mockConnManager{
		tags: map[peer.ID]string{},
	}
}

func (m mockConnManager) TagPeer(id peer.ID, s string, i int) {
}

func (m mockConnManager) UntagPeer(p peer.ID, tag string) {
}

func (m mockConnManager) UpsertTag(p peer.ID, tag string, upsert func(int) int) {
}

func (m mockConnManager) GetTagInfo(p peer.ID) *connmgrcore.TagInfo {
	return nil
}

func (m mockConnManager) TrimOpenConns(ctx context.Context) {
}

func (m mockConnManager) Notifee() libp2pnetwork.Notifiee {
	return nil
}

func (m mockConnManager) Protect(id peer.ID, tag string) {
	m.tags[id] = tag

}

func (m mockConnManager) Unprotect(id peer.ID, tag string) (protected bool) {
	_, ok := m.tags[id]
	if ok {
		delete(m.tags, id)
	}
	return ok
}

func (m mockConnManager) IsProtected(id peer.ID, tag string) (protected bool) {
	_, ok := m.tags[id]
	return ok
}

func (m mockConnManager) Close() error {
	return nil
}

func TestScorePeer(t *testing.T) {
	subnetScores := []float64{
		scoreSubnet(0, 5, 10),  // 0: Disconnected
		scoreSubnet(2, 5, 10),  // 1: Underconnected
		scoreSubnet(5, 5, 20),  // 2: Barely connected
		scoreSubnet(15, 5, 20), // 3: Well connected
		scoreSubnet(30, 5, 20), // 4: Overconnected
	}
	peers := []records.Subnets{
		{1, 1, 1, 1, 1},
		{0, 1, 1, 1, 1},
		{0, 0, 1, 1, 1},
		{0, 0, 0, 1, 1},
		{0, 0, 0, 0, 1},
		{1, 1, 1, 1, 0},
		{1, 1, 1, 0, 0},
		{1, 1, 0, 0, 0},
		{1, 0, 0, 0, 0},
		{0, 0, 0, 0, 0},
	}
	for peerIndex, p := range peers {
		score := scorePeer(p, subnetScores)
		var peerSubnets string
		for i, s := range p {
			if i > 0 {
				peerSubnets += ":"
			}
			peerSubnets += fmt.Sprint(s)
		}
		fmt.Printf("Peer #%d: Subnets(%s) -> Score(%f)\n", 1000+peerIndex, peerSubnets, score)
	}
}
