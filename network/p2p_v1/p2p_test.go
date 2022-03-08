package p2pv1

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/network/p2p_v1/topics"
	"github.com/bloxapp/ssv/protocol"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync/atomic"
	"testing"
	"time"
)

func TestP2pNetwork_SetupStart(t *testing.T) {
	threshold.Init()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	//logger := zaptest.NewLogger(t)
	logger := zap.L()

	sk, err := commons.GenNetworkKey()
	require.NoError(t, err)
	cfg := defaultMockConfig(logger, sk)
	p := New(ctx, cfg).(*p2pNetwork)
	defer func() {
		_ = p.Close()
	}()
	require.NoError(t, p.Setup())
	t.Logf("configured first peer %s", p.host.ID().String())
	require.NoError(t, p.Start())
	t.Logf("started first peer %s", p.host.ID().String())

	sk2, err := commons.GenNetworkKey()
	require.NoError(t, err)
	cfg2 := defaultMockConfig(logger, sk2)
	cfg2.TCPPort++
	cfg2.UDPPort++
	p2 := New(ctx, cfg2).(*p2pNetwork)
	defer func() {
		_ = p2.Close()
	}()
	require.NoError(t, p2.Setup())
	t.Logf("configured second peer %s", p2.host.ID().String())
	require.NoError(t, p2.Start())
	t.Logf("started second peer %s", p2.host.ID().String())

	for p.idx.State(p2.host.ID()) != peers.StateReady {
		<-time.After(time.Second)
	}
	for p2.idx.State(p.host.ID()) != peers.StateReady {
		<-time.After(time.Second)
	}

	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"}

	for _, pk := range pks {
		vpk, err := hex.DecodeString(pk)
		require.NoError(t, err)
		require.NoError(t, p.Subscribe(vpk))
		require.NoError(t, p2.Subscribe(vpk))
	}

	r1 := &dummyRouter{logger: logger.With(zap.String("who", "r1"))}
	p.UseMessageRouter(r1)
	r2 := &dummyRouter{logger: logger.With(zap.String("who", "r2"))}
	p2.UseMessageRouter(r2)

	<-time.After(time.Second)

	var net network.V1
	for i := 1; i < 5; i++ {
		msg, err := dummyMsg(pks[0], i)
		require.NoError(t, err)
		net = p
		if i%2 == 0 {
			net = p2
		}
		go func(msg protocol.SSVMessage, net network.V1) {
		broadcastLoop:
			for ctx.Err() == nil {
				err := net.Broadcast(msg)
				switch err {
				case topics.ErrInProcess:
					fallthrough
				case topics.ErrTopicNotReady:
					time.Sleep(100 * time.Millisecond)
					continue broadcastLoop
				default:
				}
				require.NoError(t, err)
				return
			}
		}(*msg, net)
	}

	<-time.After(time.Second * 4)

	require.Equal(t, uint64(4), r1.count)
	require.Equal(t, uint64(4), r2.count)
}

type dummyRouter struct {
	logger *zap.Logger
	count  uint64
}

func (r *dummyRouter) Route(message protocol.SSVMessage) {
	c := atomic.AddUint64(&r.count, 1)
	r.logger.Debug("got message",
		zap.String("identifier", hex.EncodeToString(message.GetID())),
		zap.Uint64("count", c))
}

func dummyMsg(pkHex string, height int) (*protocol.SSVMessage, error) {
	pk, err := hex.DecodeString(pkHex)
	if err != nil {
		return nil, err
	}
	id := protocol.NewIdentifier(pk, beacon.RoleTypeAttester)
	msgData := fmt.Sprintf(`{
	  "message": {
		"type": 3,
		"round": 2,
		"identifier": "%s",
		"height": %d,
		"value": "bk0iAAAAAAACAAAAAAAAAAbYXFSt2H7SQd5q5u+N0bp6PbbPTQjU25H1QnkbzTECahIBAAAAAADmi+NJfvXZ3iXp2cfs0vYVW+EgGD7DTTvr5EkLtiWq8WsSAQAAAAAAIC8dZTEdD3EvE38B9kDVWkSLy40j0T+TtSrrrBqVjo4="
	  },
	  "signature": "sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv2hMf2iRL5ZPKOSmMifHbd8yg4PeeceyN",
	  "signer_ids": [1,3,4]
	}`, id, height)
	return &protocol.SSVMessage{
		MsgType: protocol.SSVConsensusMsgType,
		ID:      id,
		Data:    []byte(msgData),
	}, nil
}

func defaultMockConfig(logger *zap.Logger, sk *ecdsa.PrivateKey) *Config {
	return &Config{
		Bootnodes:         "",
		TCPPort:           commons.DefaultTCP,
		UDPPort:           commons.DefaultUDP,
		HostAddress:       "",
		HostDNS:           "",
		RequestTimeout:    10 * time.Second,
		MaxBatchResponse:  25,
		MaxPeers:          10,
		PubSubTrace:       false,
		NetworkPrivateKey: sk,
		OperatorPublicKey: nil,
		Logger:            logger,
		Fork:              forksv1.New(),
	}
}
