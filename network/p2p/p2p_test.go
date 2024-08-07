package p2pv1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	"github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
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

	shares := []*ssvtypes.SSVShare{
		{
			Share: *spectestingutils.TestingShare(spectestingutils.Testing4SharesSet(), spectestingutils.TestingValidatorIndex),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status: eth2apiv1.ValidatorStateActiveOngoing,
					Index:  spectestingutils.TestingShare(spectestingutils.Testing4SharesSet(), spectestingutils.TestingValidatorIndex).ValidatorIndex,
				},
				Liquidated: false,
			},
		},
	}

	ln, routers, err := createNetworkAndSubscribe(t, ctx, LocalNetOptions{
		Nodes:        n,
		MinConnected: n/2 - 1,
		UseDiscv5:    false,
		Shares:       shares,
	})
	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	time.Sleep(3 * time.Second)

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
		msg1 := generateMsg(spectestingutils.Testing4SharesSet(), 1)
		msg3 := generateMsg(spectestingutils.Testing4SharesSet(), 3)
		require.NoError(t, node1.Broadcast(msg1.SSVMessage.GetID(), msg1))
		<-time.After(time.Millisecond * 20)
		require.NoError(t, node2.Broadcast(msg3.SSVMessage.GetID(), msg3))
		<-time.After(time.Millisecond * 20)
		require.NoError(t, node2.Broadcast(msg1.SSVMessage.GetID(), msg1))
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		msg1 := generateMsg(spectestingutils.Testing4SharesSet(), 1)
		msg2 := generateMsg(spectestingutils.Testing4SharesSet(), 2)
		msg3 := generateMsg(spectestingutils.Testing4SharesSet(), 3)
		require.NoError(t, err)

		time.Sleep(time.Millisecond * 20)
		require.NoError(t, node1.Broadcast(msg2.SSVMessage.GetID(), msg2))

		time.Sleep(time.Millisecond * 20)
		require.NoError(t, node2.Broadcast(msg1.SSVMessage.GetID(), msg1))
		require.NoError(t, node1.Broadcast(msg3.SSVMessage.GetID(), msg3))
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
		assert.GreaterOrEqual(t, atomic.LoadUint64(&r.count), uint64(2), "router %d", r.i)
	}
}

func TestP2pNetwork_Stream(t *testing.T) {
	t.Skip("will be removed in https://github.com/ssvlabs/ssv/pull/1544")

	n := 12
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.TestLogger(t)
	defer cancel()

	shares := []*ssvtypes.SSVShare{
		{
			Share: *spectestingutils.TestingShare(spectestingutils.Testing4SharesSet(), spectestingutils.TestingValidatorIndex),
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Status: eth2apiv1.ValidatorStateActiveOngoing,
					Index:  spectestingutils.TestingShare(spectestingutils.Testing4SharesSet(), spectestingutils.TestingValidatorIndex).ValidatorIndex,
				},
				Liquidated: false,
			},
		},
	}

	ln, _, err := createNetworkAndSubscribe(t, ctx, LocalNetOptions{
		Nodes:        n,
		MinConnected: n/2 - 1,
		UseDiscv5:    false,
		Shares:       shares,
	})

	defer func() {
		for _, node := range ln.Nodes {
			require.NoError(t, node.(*p2pNetwork).Close())
		}
	}()
	require.NoError(t, err)
	require.Len(t, ln.Nodes, n)

	mid := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType(), shares[0].ValidatorPubKey[:], spectypes.RoleCommittee)
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

func generateMsg(ks *spectestingutils.TestKeySet, round specqbft.Round) *spectypes.SignedSSVMessage {
	netCfg := networkconfig.TestNetwork
	height := specqbft.Height(netCfg.Beacon.EstimatedCurrentSlot())

	share := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: eth2apiv1.ValidatorStateActiveOngoing,
				Index:  spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex).ValidatorIndex,
			},
			Liquidated: false,
		},
	}
	committeeID := share.CommitteeID()

	fullData := spectestingutils.TestingQBFTFullData

	encodedCommitteeID := append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
	committeeIdentifier := spectypes.NewMsgID(netCfg.DomainType(), encodedCommitteeID, spectypes.RoleCommittee)

	qbftMessage := &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     height,
		Round:      round,
		Identifier: committeeIdentifier[:],
		Root:       sha256.Sum256(fullData),

		RoundChangeJustification: [][]byte{},
		PrepareJustification:     [][]byte{},
	}

	leader := roundLeader(ks, height, round)
	signedSSVMessage := spectestingutils.SignQBFTMsg(ks.OperatorKeys[leader], leader, qbftMessage)
	signedSSVMessage.FullData = fullData

	return signedSSVMessage
}

func roundLeader(ks *spectestingutils.TestKeySet, height specqbft.Height, round specqbft.Round) types.OperatorID {
	share := spectestingutils.TestingShare(ks, 1)

	firstRoundIndex := 0
	if height != specqbft.FirstHeight {
		firstRoundIndex += int(height) % len(share.Committee)
	}

	index := (firstRoundIndex + int(round) - int(specqbft.FirstRound)) % len(share.Committee)
	return share.Committee[index].Signer
}

func dummyMsg(t *testing.T, pkHex string, height int, role spectypes.RunnerRole) (spectypes.MessageID, *spectypes.SignedSSVMessage) {
	pk, err := hex.DecodeString(pkHex)
	require.NoError(t, err)
	id := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType(), pk, role)

	qbftMessage := &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     specqbft.Height(height),
		Round:      2,
		Identifier: id[:],
		Root:       [32]byte{0x1, 0x2, 0x3},
	}

	encodedQBFTMessage, err := qbftMessage.Encode()
	require.NoError(t, err)

	signedSSVMsg := &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   id,
			Data:    encodedQBFTMessage,
		},
		Signatures:  [][]byte{[]byte("sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv")},
		OperatorIDs: []spectypes.OperatorID{1, 3, 4},
	}

	return id, signedSSVMsg
}

type dummyRouter struct {
	count uint64
	i     int
}

func (r *dummyRouter) Route(_ context.Context, _ network.DecodedSSVMessage) {
	atomic.AddUint64(&r.count, 1)
}

func createNetworkAndSubscribe(t *testing.T, ctx context.Context, options LocalNetOptions) (*LocalNet, []*dummyRouter, error) {
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
	for _, share := range options.Shares {
		for _, node := range ln.Nodes {
			wg.Add(1)
			go func(node network.P2PNetwork, vpk spectypes.ValidatorPK) {
				defer wg.Done()
				if err := node.Subscribe(vpk); err != nil {
					logger.Warn("could not subscribe to topic", zap.Error(err))
				}
			}(node, share.ValidatorPubKey)
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

func (n *p2pNetwork) LastDecided(logger *zap.Logger, mid spectypes.MessageID) ([]p2pprotocol.SyncResult, error) {
	const (
		minPeers = 3
		waitTime = time.Second * 24
	)
	if !n.isReady() {
		return nil, p2pprotocol.ErrNetworkIsNotReady
	}
	pid, maxPeers := commons.ProtocolID(p2pprotocol.LastDecidedProtocol)
	peers, err := waitSubsetOfPeers(logger, n.getSubsetOfPeers, mid.GetDutyExecutorID(), minPeers, maxPeers, waitTime, allPeersFilter)
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

// getSubsetOfPeers returns a subset of the peers from that topic
func (n *p2pNetwork) getSubsetOfPeers(logger *zap.Logger, senderID []byte, maxPeers int, filter func(peer.ID) bool) (peers []peer.ID, err error) {
	var ps []peer.ID
	seen := make(map[peer.ID]struct{})
	topics := commons.ValidatorTopicID(senderID)
	for _, topic := range topics {
		ps, err = n.topicsCtrl.Peers(topic)
		if err != nil {
			continue
		}
		for _, p := range ps {
			if _, ok := seen[p]; !ok && filter(p) {
				peers = append(peers, p)
				seen[p] = struct{}{}
			}
		}
	}
	// if we seen some peers, ignore the error
	if err != nil && len(seen) == 0 {
		return nil, errors.Wrapf(err, "could not read peers for validator %s", hex.EncodeToString(senderID))
	}
	if len(peers) == 0 {
		return nil, nil
	}
	if maxPeers > len(peers) {
		maxPeers = len(peers)
	} else {
		rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
	}
	return peers[:maxPeers], nil
}

func registerHandler(logger *zap.Logger, node network.P2PNetwork, mid spectypes.MessageID, height specqbft.Height, round specqbft.Round, counter *int64, errors chan<- error) {
	node.RegisterHandlers(logger, &p2pprotocol.SyncHandler{
		Protocol: p2pprotocol.LastDecidedProtocol,
		Handler: func(message *spectypes.SSVMessage) (*spectypes.SSVMessage, error) {
			atomic.AddInt64(counter, 1)
			sm := genesisspecqbft.SignedMessage{
				Signature: make([]byte, 96),
				Signers:   []spectypes.OperatorID{1, 2, 3},
				Message: genesisspecqbft.Message{
					MsgType:    genesisspecqbft.CommitMsgType,
					Height:     genesisspecqbft.Height(height),
					Round:      genesisspecqbft.Round(round),
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
