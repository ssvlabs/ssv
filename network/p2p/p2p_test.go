package p2pv1

import (
	"context"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
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
	t.Skip("need to implement validator store")

	n := 4
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pks := []string{"8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0"}
	ln, routers, err := createNetworkAndSubscribe(t, ctx, LocalNetOptions{
		Nodes:        n,
		MinConnected: n/2 - 1,
		UseDiscv5:    false,
	}, pks...)
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

func dummyMsgCommittee(t *testing.T, pkHex string, height int) (spectypes.MessageID, *spectypes.SignedSSVMessage) {
	return dummyMsg(t, pkHex, height, spectypes.RoleCommittee)
}

func dummyMsg(t *testing.T, pkHex string, height int, role spectypes.RunnerRole) (spectypes.MessageID, *spectypes.SignedSSVMessage) {
	pk, err := hex.DecodeString(pkHex)
	require.NoError(t, err)
	id := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType(), pk, role)
	signedMsg := &genesisspecqbft.SignedMessage{
		Message: genesisspecqbft.Message{
			MsgType:    genesisspecqbft.CommitMsgType,
			Round:      2,
			Identifier: id[:],
			Height:     genesisspecqbft.Height(height),
			Root:       [32]byte{0x1, 0x2, 0x3},
		},
		Signature: []byte("sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv"),
		Signers:   []spectypes.OperatorID{1, 3, 4},
	}
	data, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   id,
		Data:    data,
	}

	sig, err := dummySignSSVMessage(ssvMsg)
	require.NoError(t, err)

	signedSSVMsg := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{1},
		SSVMessage:  ssvMsg,
	}

	return id, signedSSVMsg
}

func dummySignSSVMessage(_ *spectypes.SSVMessage) ([]byte, error) {
	return []byte{}, nil
}

type dummyRouter struct {
	count uint64
	i     int
}

func (r *dummyRouter) Route(_ context.Context, _ network.DecodedSSVMessage) {
	atomic.AddUint64(&r.count, 1)
}

func createNetworkAndSubscribe(t *testing.T, ctx context.Context, options LocalNetOptions, pks ...string) (*LocalNet, []*dummyRouter, error) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	ln, err := CreateAndStartLocalNet(ctx, logger.Named("createNetworkAndSubscribe"), options)
	if err != nil {
		return nil, nil, err
	}
	if len(ln.Nodes) != options.Nodes {
		return nil, nil, errors.Errorf("only %d peers created, expected %d", len(ln.Nodes), options.Nodes)
	}

	logger.Debug("created local network")

	routers := make([]*dummyRouter, options.Nodes)
	for i, node := range ln.Nodes {
		routers[i] = &dummyRouter{
			i: i,
		}
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
			go func(node network.P2PNetwork, vpk spectypes.ValidatorPK) {
				defer wg.Done()
				if err := node.Subscribe(vpk); err != nil {
					logger.Warn("could not subscribe to topic", zap.Error(err))
				}
			}(node, spectypes.ValidatorPK(vpk))
		}
	}
	wg.Wait()
	// let the nodes subscribe
	for {
		noPeers := false
		for _, node := range ln.Nodes {
			peers, _ := node.PeersByTopic()
			if len(peers) < 2 {
				noPeers = true
			}
		}
		if noPeers {
			noPeers = false
			time.Sleep(time.Second * 1)
			continue
		}
		break
	}

	return ln, routers, nil
}
