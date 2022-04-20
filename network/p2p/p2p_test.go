package p2pv1

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestP2pNetwork_SubscribeBroadcast(t *testing.T) {
	n := 4
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"}

	ln, routers, err := createNetworkAndSubscribe(ctx, t, n, pks, forksprotocol.V1ForkVersion)
	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	msg1, err := dummyMsg(pks[0], 1)
	require.NoError(t, err)
	msg2, err := dummyMsg(pks[1], 2)
	require.NoError(t, err)
	msg3, err := dummyMsg(pks[0], 3)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(t, ln.Nodes[1].Broadcast(*msg1))
		<-time.After(time.Millisecond * 10)
		require.NoError(t, ln.Nodes[2].Broadcast(*msg3))
		<-time.After(time.Millisecond * 2)
		require.NoError(t, ln.Nodes[1].Broadcast(*msg1))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-time.After(time.Millisecond * 10)
		require.NoError(t, ln.Nodes[1].Broadcast(*msg2))
		<-time.After(time.Millisecond * 2)
		require.NoError(t, ln.Nodes[2].Broadcast(*msg1))
		require.NoError(t, ln.Nodes[1].Broadcast(*msg3))
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

	for _, node := range ln.Nodes {
		require.NoError(t, node.(*p2pNetwork).Close())
	}
}

func TestP2pNetwork_OnFork(t *testing.T) {
	n := 4
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"}

	ln, routers, err := createNetworkAndSubscribe(ctx, t, n, pks, forksprotocol.V0ForkVersion)
	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		msgs, err := dummyMsgs(1, pks[0], pks[1])
		require.NoError(t, err)
		require.NoError(t, ln.Nodes[0].Broadcast(*msgs[0]))
		require.NoError(t, ln.Nodes[1].Broadcast(*msgs[1]))
		require.NoError(t, ln.Nodes[2].Broadcast(*msgs[1]))
		<-time.After(time.Second * 2)
	}()
	wg.Wait()

	for _, r := range routers {
		require.GreaterOrEqual(t, atomic.LoadUint64(&r.count), uint64(2), "router", r.i)
	}

	for _, p := range ln.Nodes {
		wg.Add(1)
		go func(handler forksprotocol.ForkHandler) {
			defer wg.Done()
			require.NoError(t, handler.OnFork(forksprotocol.V1ForkVersion))
			<-time.After(time.Second * 3)
		}(p.(forksprotocol.ForkHandler))
	}
	wg.Wait()

	// sending messages after fork
	wg.Add(1)
	go func() {
		defer wg.Done()
		msgs, err := dummyMsgs(2, pks[0], pks[1])
		require.NoError(t, err)
		require.NoError(t, ln.Nodes[0].Broadcast(*msgs[0]))
		require.NoError(t, ln.Nodes[1].Broadcast(*msgs[1]))
		require.NoError(t, ln.Nodes[2].Broadcast(*msgs[1]))
		<-time.After(time.Second * 3)
	}()
	wg.Wait()

	for _, r := range routers {
		require.GreaterOrEqual(t, atomic.LoadUint64(&r.count), uint64(3), "router", r.i)
	}
}

func createNetworkAndSubscribe(ctx context.Context, t *testing.T, n int, pks []string, forkVersion forksprotocol.ForkVersion) (*LocalNet, []*dummyRouter, error) {
	loggerFactory := func(who string) *zap.Logger {
		//logger := zaptest.NewLogger(t).With(zap.String("who", who))
		logger := zap.L().With(zap.String("who", who))
		return logger
	}
	ln, err := CreateAndStartLocalNet(ctx, loggerFactory, forkVersion, n, n-1, false)
	if err != nil {
		return nil, nil, err
	}
	if len(ln.Nodes) != n {
		return nil, nil, errors.Errorf("only %d peers created, expected %d", len(ln.Nodes), n)
	}

	routers := make([]*dummyRouter, n)

	// for now, skip routers for v0
	//if forkVersion != forksprotocol.V0ForkVersion {
	for i, node := range ln.Nodes {
		routers[i] = &dummyRouter{i: i, logger: loggerFactory(fmt.Sprintf("msgRouter-%d", i))}
		node.UseMessageRouter(routers[i])
	}
	//}

	for _, pk := range pks {
		vpk, err := hex.DecodeString(pk)
		if err != nil {
			return nil, nil, errors.Wrap(err, "could not decode validator public key")
		}
		for _, node := range ln.Nodes {
			if err := node.Subscribe(vpk); err != nil {
				return nil, nil, err
			}
		}
	}
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
	logger *zap.Logger
	count  uint64
	i      int
}

func (r *dummyRouter) Route(message message.SSVMessage) {
	c := atomic.AddUint64(&r.count, 1)
	r.logger.Debug("got message",
		zap.String("identifier", hex.EncodeToString(message.GetIdentifier())),
		zap.Uint64("count", c))
}

func dummyMsgs(h int, pks ...string) ([]*message.SSVMessage, error) {
	var msgs []*message.SSVMessage
	for _, pk := range pks {
		msg, err := dummyMsg(pk, h)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

func dummyMsg(pkHex string, height int) (*message.SSVMessage, error) {
	pk, err := hex.DecodeString(pkHex)
	if err != nil {
		return nil, err
	}
	id := message.NewIdentifier(pk, message.RoleTypeAttester)
	v0SignedMsg := &proto.SignedMessage{
		Message: &proto.Message{
			Type:      3,
			Round:     2,
			Lambda:    id,
			SeqNumber: uint64(height),
			Value:     []byte("bk0iAAAAAAACAAAAAAAAAAbYXFSt2H7SQd5q5u+N0bp6PbbPTQjU25H1QnkbzTECahIBAAAAAADmi+NJfvXZ3iXp2cfs0vYVW+EgGD7DTTvr5EkLtiWq8WsSAQAAAAAAIC8dZTEdD3EvE38B9kDVWkSLy40j0T+TtSrrrBqVjo4="),
		},
		Signature: []byte("sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv2hMf2iRL5ZPKOSmMifHbd8yg4PeeceyN"),
		SignerIds: []uint64{1, 3, 4},
	}
	data, err := json.Marshal(v0SignedMsg)
	if err != nil {
		return nil, err
	}
	return &message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      id,
		Data:    data,
	}, nil
}
