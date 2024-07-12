package p2pv1

import (
	"context"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
)

func TestGetMaxPeers(t *testing.T) {
	n := &p2pNetwork{
		cfg: &Config{MaxPeers: 40, TopicMaxPeers: 8},
	}

	require.Equal(t, 40, n.getMaxPeers(""))
	require.Equal(t, 8, n.getMaxPeers("100"))
}

func TestP2pNetwork_SubscribeBroadcast(t *testing.T) {
	n := 4
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ks := spectestingutils.Testing4SharesSet()
	pks := []string{"8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0"}
	ln, routers, err := CreateNetworkAndSubscribeFromKeySet(t, ctx, LocalNetOptions{
		Nodes:        n,
		MinConnected: n/2 - 1,
		UseDiscv5:    false,
	}, ks, pks...)
	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	defer func() {
		for _, node := range ln.Nodes {
			require.NoError(t, node.(*p2pNetwork).Close())
		}
	}()

	node1, node2 := ln.Nodes[1], ln.Nodes[2]

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		msgID1, msg1 := dummyMsgCommittee(t, pks[0], 1)
		msgID3, msg3 := dummyMsgCommittee(t, pks[0], 3)
		require.NoError(t, node1.Broadcast(msgID1, msg1))
		<-time.After(time.Millisecond * 10)
		require.NoError(t, node2.Broadcast(msgID3, msg3))
		<-time.After(time.Millisecond * 2)
		require.NoError(t, node2.Broadcast(msgID1, msg1))
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()
		msgID1, msg1 := dummyMsgCommittee(t, pks[0], 1)
		msgID2, msg2 := dummyMsgCommittee(t, pks[1], 2)
		msgID3, msg3 := dummyMsgCommittee(t, pks[0], 3)
		require.NoError(t, err)
		time.Sleep(time.Millisecond * 10)
		require.NoError(t, node1.Broadcast(msgID2, msg2))
		time.Sleep(time.Millisecond * 2)
		require.NoError(t, node2.Broadcast(msgID1, msg1))
		require.NoError(t, node1.Broadcast(msgID3, msg3))
	}()

	wg.Wait()

	// waiting for messages
	wg.Add(1)
	go func() {
		ct, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		defer wg.Done()
		for _, r := range routers {
			for ct.Err() == nil && atomic.LoadUint64(&r.count) < uint64(2) {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	wg.Wait()

	for _, r := range routers {
		require.GreaterOrEqual(t, atomic.LoadUint64(&r.count), uint64(2), "router", r.i)
	}

	<-time.After(time.Millisecond * 10)
}

func TestP2pNetwork_Stream(t *testing.T) {
	n := 12
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.TestLogger(t)
	defer cancel()

	pkHex := "8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0"
	ks := spectestingutils.Testing4SharesSet()
	ln, _, err := CreateNetworkAndSubscribeFromKeySet(t, ctx, LocalNetOptions{
		Nodes:        n,
		MinConnected: n/2 - 1,
		UseDiscv5:    false,
	}, ks, pkHex)

	defer func() {
		for _, node := range ln.Nodes {
			require.NoError(t, node.(*p2pNetwork).Close())
		}
	}()
	require.NoError(t, err)
	require.Len(t, ln.Nodes, n)

	pk, err := hex.DecodeString(pkHex)
	require.NoError(t, err)

	mid := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType(), pk, spectypes.RoleCommittee)
	rounds := []specqbft.Round{
		1, 1, 1,
		1, 2, 2,
		3, 3, 1,
		1, 1, 1,
	}
	heights := []specqbft.Height{
		0, 0, 2,
		10, 20, 20,
		23, 23, 1,
		1, 1, 1,
	}
	msgCounter := int64(0)
	errors := make(chan error, len(ln.Nodes))
	for i, node := range ln.Nodes {
		registerHandler(logger, node, mid, heights[i], rounds[i], &msgCounter, errors)
	}

	<-time.After(time.Second)

	node := ln.Nodes[0]
	res, err := node.(*p2pNetwork).LastDecided(logger, mid)
	require.NoError(t, err)
	select {
	case err := <-errors:
		require.NoError(t, err)
	default:
	}
	require.GreaterOrEqual(t, len(res), 2) // got at least 2 results
	require.LessOrEqual(t, len(res), 6)    // less than 6 unique heights
	require.GreaterOrEqual(t, msgCounter, int64(2))

}

func TestWaitSubsetOfPeers(t *testing.T) {
	logger, _ := zap.NewProduction()

	tests := []struct {
		name             string
		minPeers         int
		maxPeers         int
		timeout          time.Duration
		expectedPeersLen int
		expectedErr      string
	}{
		{"Valid input", 5, 5, time.Millisecond * 30, 5, ""},
		{"Zero minPeers", 0, 10, time.Millisecond * 30, 0, ""},
		{"maxPeers less than minPeers", 10, 5, time.Millisecond * 30, 0, "minPeers should not be greater than maxPeers"},
		{"Negative minPeers", -1, 10, time.Millisecond * 30, 0, "minPeers and maxPeers should not be negative"},
		{"Negative timeout", 10, 50, time.Duration(-1), 0, "timeout should be positive"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vpk := spectypes.ValidatorPK{} // replace with a valid value
			// The mock function increments the number of peers by 1 for each call, up to maxPeers
			peersCount := 0
			start := time.Now()
			mockGetSubsetOfPeers := func(logger *zap.Logger, senderID []byte, maxPeers int, filter func(peer.ID) bool) (peers []peer.ID, err error) {
				if tt.minPeers == 0 {
					return []peer.ID{}, nil
				}

				peersCount++
				if peersCount > maxPeers || time.Since(start) > (tt.timeout-tt.timeout/5) {
					peersCount = maxPeers
				}
				peers = make([]peer.ID, peersCount)
				return peers, nil
			}

			peers, err := waitSubsetOfPeers(logger, mockGetSubsetOfPeers, vpk[:], tt.minPeers, tt.maxPeers, tt.timeout, nil)
			if err != nil && err.Error() != tt.expectedErr {
				t.Errorf("waitSubsetOfPeers() error = %v, wantErr %v", err, tt.expectedErr)
				return
			}

			if len(peers) != tt.expectedPeersLen {
				t.Errorf("waitSubsetOfPeers() len(peers) = %v, want %v", len(peers), tt.expectedPeersLen)
			}
		})
	}
}
