package p2pv1

import (
	"context"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	protcolp2p "github.com/bloxapp/ssv/protocol/p2p"
	"github.com/bloxapp/ssv/utils/logex"
)

func TestGetMaxPeers(t *testing.T) {
	n := &p2pNetwork{
		cfg:  &Config{MaxPeers: 40, TopicMaxPeers: 8},
		fork: forksfactory.NewFork(forksprotocol.GenesisForkVersion),
	}

	require.Equal(t, 40, n.getMaxPeers(""))
	require.Equal(t, 8, n.getMaxPeers("100"))
}

func TestP2pNetwork_SubscribeBroadcast(t *testing.T) {
	n := 4
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"}

	ln, routers, err := createNetworkAndSubscribe(t, ctx, n, forksprotocol.GenesisForkVersion, pks...)
	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	node1, node2 := ln.Nodes[1], ln.Nodes[2]

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		msg1, err := dummyMsg(pks[0], 1)
		require.NoError(t, err)
		msg3, err := dummyMsg(pks[0], 3)
		require.NoError(t, err)
		require.NoError(t, node1.Broadcast(msg1))
		<-time.After(time.Millisecond * 10)
		require.NoError(t, node2.Broadcast(msg3))
		<-time.After(time.Millisecond * 2)
		require.NoError(t, node2.Broadcast(msg1))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		msg1, err := dummyMsg(pks[0], 1)
		require.NoError(t, err)
		msg2, err := dummyMsg(pks[1], 2)
		require.NoError(t, err)
		msg3, err := dummyMsg(pks[0], 3)
		require.NoError(t, err)
		<-time.After(time.Millisecond * 10)
		require.NoError(t, node1.Broadcast(msg2))
		<-time.After(time.Millisecond * 2)
		require.NoError(t, node2.Broadcast(msg1))
		require.NoError(t, node1.Broadcast(msg3))
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

	for _, node := range ln.Nodes {
		require.NoError(t, node.(*p2pNetwork).Close())
	}
}

func TestP2pNetwork_Stream(t *testing.T) {
	n := 12
	ctx, cancel := context.WithCancel(context.Background())
	logger := logex.TestLogger(t)
	defer cancel()

	pkHex := "b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400"

	ln, _, err := createNetworkAndSubscribe(t, ctx, n, forksprotocol.GenesisForkVersion, pkHex)
	require.NoError(t, err)
	require.Len(t, ln.Nodes, n)

	pk, err := hex.DecodeString(pkHex)
	require.NoError(t, err)
	mid := spectypes.NewMsgID(pk, spectypes.BNRoleAttester)
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
	for i, node := range ln.Nodes {
		registerHandler(logger, node, mid, heights[i], rounds[i], &msgCounter)
	}

	<-time.After(time.Second)

	node := ln.Nodes[0]
	res, err := node.LastDecided(logger, mid)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(res), 2) // got at least 2 results
	require.LessOrEqual(t, len(res), 6)    // less than 6 unique heights
	require.GreaterOrEqual(t, msgCounter, int64(2))
}

func registerHandler(logger *zap.Logger, node network.P2PNetwork, mid spectypes.MessageID, height specqbft.Height, round specqbft.Round, counter *int64) {
	node.RegisterHandlers(logger, &protcolp2p.SyncHandler{
		Protocol: protcolp2p.LastDecidedProtocol,
		Handler: func(message *spectypes.SSVMessage) (*spectypes.SSVMessage, error) {
			atomic.AddInt64(counter, 1)
			sm := specqbft.SignedMessage{
				Signature: []byte("xxx"),
				Signers:   []spectypes.OperatorID{1, 2, 3},
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Height:     height,
					Round:      round,
					Identifier: mid[:],
					Data:       []byte("dummy change round message"),
				},
			}
			data, err := sm.Encode()
			if err != nil {
				return nil, err
			}
			return &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   mid,
				Data:    data,
			}, nil
		},
	})
}

func createNetworkAndSubscribe(t *testing.T, ctx context.Context, n int, forkVersion forksprotocol.ForkVersion, pks ...string) (*LocalNet, []*dummyRouter, error) {
	logger := logex.TestLogger(t)
	ln, err := CreateAndStartLocalNet(ctx, logger.Named("createNetworkAndSubscribe"), forkVersion, n, n/2-1, false)
	if err != nil {
		return nil, nil, err
	}
	if len(ln.Nodes) != n {
		return nil, nil, errors.Errorf("only %d peers created, expected %d", len(ln.Nodes), n)
	}

	logger.Debug("created local network")

	routers := make([]*dummyRouter, n)
	// for now, skip routers for v0
	// if forkVersion != forksprotocol.GenesisForkVersion {
	for i, node := range ln.Nodes {
		routers[i] = &dummyRouter{i: i}
		node.UseMessageRouter(routers[i])
	}

	logger.Debug("subscribing to topics")

	var wg sync.WaitGroup
	for _, pk := range pks {
		vpk, err := hex.DecodeString(pk)
		if err != nil {
			return nil, nil, errors.Wrap(err, "could not decode validator public key")
		}
		for _, node := range ln.Nodes {
			wg.Add(1)
			go func(node network.P2PNetwork, vpk []byte) {
				defer wg.Done()
				if err := node.Subscribe(vpk); err != nil {
					logger.Warn("could not subscribe to topic", zap.Error(err))
				}
			}(node, vpk)
		}
	}
	wg.Wait()
	// let the nodes subscribe
	<-time.After(time.Second)
	for _, pk := range pks {
		vpk, err := hex.DecodeString(pk)
		if err != nil {
			return nil, nil, errors.Wrap(err, "could not decode validator public key")
		}
		for _, node := range ln.Nodes {
			peers := make([]peer.ID, 0)
			for len(peers) < 2 {
				peers, err = node.Peers(vpk)
				if err != nil {
					return nil, nil, err
				}
				time.Sleep(time.Millisecond * 100)
			}
		}
	}

	return ln, routers, nil
}

type dummyRouter struct {
	count uint64
	i     int
}

func (r *dummyRouter) Route(logger *zap.Logger, message spectypes.SSVMessage) {
	c := atomic.AddUint64(&r.count, 1)
	logger.Debug("got message",
		zap.String("identifier", message.GetID().String()),
		zap.Uint64("count", c))
}

func dummyMsg(pkHex string, height int) (*spectypes.SSVMessage, error) {
	pk, err := hex.DecodeString(pkHex)
	if err != nil {
		return nil, err
	}
	id := spectypes.NewMsgID(pk, spectypes.BNRoleAttester)
	signedMsg := &specqbft.SignedMessage{
		Message: &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Round:      2,
			Identifier: id[:],
			Height:     specqbft.Height(height),
			Data:       []byte("bk0iAAAAAAACAAAAAAAAAAbYXFSt2H7SQd5q5u+N0bp6PbbPTQjU25H1QnkbzTECahIBAAAAAADmi+NJfvXZ3iXp2cfs0vYVW+EgGD7DTTvr5EkLtiWq8WsSAQAAAAAAIC8dZTEdD3EvE38B9kDVWkSLy40j0T+TtSrrrBqVjo4="),
		},
		Signature: []byte("sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv2hMf2iRL5ZPKOSmMifHbd8yg4PeeceyN"),
		Signers:   []spectypes.OperatorID{1, 3, 4},
	}
	data, err := signedMsg.Encode()
	if err != nil {
		return nil, err
	}
	return &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   id,
		Data:    data,
	}, nil
}
