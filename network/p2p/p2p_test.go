package p2pv1

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
)

func TestRSAUsage(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	testMessage := []byte("message")

	hash := sha256.Sum256(testMessage)

	signature, err := rsa.SignPKCS1v15(nil, privateKey, crypto.SHA256, hash[:])
	require.NoError(t, err)

	publicKey := &privateKey.PublicKey

	pubKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		fmt.Println("Error marshalling public key:", err)
		return
	}

	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	const operatorID = spectypes.OperatorID(0x12345678)
	encodedSignedSSVMessage := commons.EncodeSignedSSVMessage(testMessage, operatorID, signature)

	decodedMessage, decodedOperatorID, decodedSignature, err := commons.DecodeSignedSSVMessage(encodedSignedSSVMessage)
	require.NoError(t, err)
	require.Equal(t, operatorID, decodedOperatorID)
	require.Equal(t, signature, decodedSignature)

	messageHash := sha256.Sum256(decodedMessage)

	block, rest := pem.Decode(pubPEM)
	require.NotNil(t, block)
	require.Empty(t, rest, "extra data after PEM decoding")

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	require.NoError(t, err)

	rsaPubKey, ok := pub.(*rsa.PublicKey)
	require.True(t, ok)

	require.NoError(t, rsa.VerifyPKCS1v15(rsaPubKey, crypto.SHA256, messageHash[:], decodedSignature))
	require.Equal(t, testMessage, decodedMessage)
}

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

	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"}
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
		msg1 := dummyMsgAttester(t, pks[0], 1)
		msg3 := dummyMsgAttester(t, pks[0], 3)
		require.NoError(t, node1.Broadcast(msg1))
		<-time.After(time.Millisecond * 10)
		require.NoError(t, node2.Broadcast(msg3))
		<-time.After(time.Millisecond * 2)
		require.NoError(t, node2.Broadcast(msg1))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		msg1 := dummyMsgAttester(t, pks[0], 1)
		msg2 := dummyMsgAttester(t, pks[1], 2)
		msg3 := dummyMsgAttester(t, pks[0], 3)
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
}

func TestP2pNetwork_Stream(t *testing.T) {
	n := 12
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.TestLogger(t)
	defer cancel()

	pkHex := "b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400"

	ln, _, err := createNetworkAndSubscribe(t, ctx, LocalNetOptions{
		Nodes:        n,
		MinConnected: n/2 - 1,
		UseDiscv5:    false,
	}, pkHex)

	defer func() {
		for _, node := range ln.Nodes {
			require.NoError(t, node.(*p2pNetwork).Close())
		}
	}()
	require.NoError(t, err)
	require.Len(t, ln.Nodes, n)

	pk, err := hex.DecodeString(pkHex)
	require.NoError(t, err)
	mid := spectypes.NewMsgID(networkconfig.TestNetwork.Domain, pk, spectypes.BNRoleAttester)
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
			mockGetSubsetOfPeers := func(logger *zap.Logger, vpk spectypes.ValidatorPK, maxPeers int, filter func(peer.ID) bool) (peers []peer.ID, err error) {
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

			peers, err := waitSubsetOfPeers(logger, mockGetSubsetOfPeers, vpk, tt.minPeers, tt.maxPeers, tt.timeout, nil)
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

func (n *p2pNetwork) LastDecided(logger *zap.Logger, mid spectypes.MessageID) ([]p2pprotocol.SyncResult, error) {
	const (
		minPeers = 3
		waitTime = time.Second * 24
	)
	if !n.isReady() {
		return nil, p2pprotocol.ErrNetworkIsNotReady
	}
	pid, maxPeers := commons.ProtocolID(p2pprotocol.LastDecidedProtocol)
	peers, err := waitSubsetOfPeers(logger, n.getSubsetOfPeers, mid.GetPubKey(), minPeers, maxPeers, waitTime, allPeersFilter)
	if err != nil {
		return nil, errors.Wrap(err, "could not get subset of peers")
	}
	return n.makeSyncRequest(logger, peers, mid, pid, &message.SyncMessage{
		Params: &message.SyncParams{
			Identifier: mid,
		},
		Protocol: message.LastDecidedType,
	})
}

func registerHandler(logger *zap.Logger, node network.P2PNetwork, mid spectypes.MessageID, height specqbft.Height, round specqbft.Round, counter *int64, errors chan<- error) {
	node.RegisterHandlers(logger, &p2pprotocol.SyncHandler{
		Protocol: p2pprotocol.LastDecidedProtocol,
		Handler: func(message *spectypes.SSVMessage) (*spectypes.SSVMessage, error) {
			atomic.AddInt64(counter, 1)
			sm := specqbft.SignedMessage{
				Signature: make([]byte, 96),
				Signers:   []spectypes.OperatorID{1, 2, 3},
				Message: specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Height:     height,
					Round:      round,
					Identifier: mid[:],
					Root:       [32]byte{1, 2, 3},
				},
			}
			data, err := sm.Encode()
			if err != nil {
				errors <- err
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

func (r *dummyRouter) Route(_ context.Context, _ *queue.DecodedSSVMessage) {
	atomic.AddUint64(&r.count, 1)
}

func dummyMsg(t *testing.T, pkHex string, height int, role spectypes.BeaconRole) *spectypes.SSVMessage {
	pk, err := hex.DecodeString(pkHex)
	require.NoError(t, err)
	id := spectypes.NewMsgID(networkconfig.TestNetwork.Domain, pk, role)
	signedMsg := &specqbft.SignedMessage{
		Message: specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Round:      2,
			Identifier: id[:],
			Height:     specqbft.Height(height),
			Root:       [32]byte{0x1, 0x2, 0x3},
		},
		Signature: []byte("sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv"),
		Signers:   []spectypes.OperatorID{1, 3, 4},
	}
	data, err := signedMsg.Encode()
	require.NoError(t, err)
	return &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   id,
		Data:    data,
	}
}

func dummyMsgAttester(t *testing.T, pkHex string, height int) *spectypes.SSVMessage {
	return dummyMsg(t, pkHex, height, spectypes.BNRoleAttester)
}
