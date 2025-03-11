package p2pv1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
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
			Share:      *spectestingutils.TestingShare(spectestingutils.Testing4SharesSet(), spectestingutils.TestingValidatorIndex),
			Status:     eth2apiv1.ValidatorStateActiveOngoing,
			Liquidated: false,
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
		msgCommittee1 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 1)
		msgCommittee3 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 3)
		msgProposer := generateValidatorMsg(spectestingutils.Testing4SharesSet(), 4, spectypes.RoleProposer)
		msgSyncCommitteeContribution := generateValidatorMsg(spectestingutils.Testing4SharesSet(), 5, spectypes.RoleSyncCommitteeContribution)
		msgRoleVoluntaryExit := generateValidatorMsg(spectestingutils.Testing4SharesSet(), 6, spectypes.RoleVoluntaryExit)

		require.NoError(t, node1.Broadcast(msgCommittee1.SSVMessage.GetID(), msgCommittee1))
		<-time.After(time.Millisecond * 20)
		require.NoError(t, node2.Broadcast(msgCommittee3.SSVMessage.GetID(), msgCommittee3))
		<-time.After(time.Millisecond * 20)
		require.NoError(t, node2.Broadcast(msgCommittee1.SSVMessage.GetID(), msgCommittee1))
		<-time.After(time.Millisecond * 20)
		require.NoError(t, node2.Broadcast(msgProposer.SSVMessage.GetID(), msgProposer))
		<-time.After(time.Millisecond * 20)
		require.NoError(t, node2.Broadcast(msgSyncCommitteeContribution.SSVMessage.GetID(), msgSyncCommitteeContribution))
		<-time.After(time.Millisecond * 20)
		require.NoError(t, node1.Broadcast(msgRoleVoluntaryExit.SSVMessage.GetID(), msgRoleVoluntaryExit))
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		msgCommittee1 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 1)
		msgCommittee2 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 2)
		msgCommittee3 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 3)
		msgProposer := generateValidatorMsg(spectestingutils.Testing4SharesSet(), 4, spectypes.RoleProposer)
		msgSyncCommitteeContribution := generateValidatorMsg(spectestingutils.Testing4SharesSet(), 5, spectypes.RoleSyncCommitteeContribution)
		msgRoleVoluntaryExit := generateValidatorMsg(spectestingutils.Testing4SharesSet(), 6, spectypes.RoleVoluntaryExit)

		require.NoError(t, err)

		time.Sleep(time.Millisecond * 20)
		require.NoError(t, node1.Broadcast(msgCommittee2.SSVMessage.GetID(), msgCommittee2))

		time.Sleep(time.Millisecond * 20)
		require.NoError(t, node2.Broadcast(msgCommittee1.SSVMessage.GetID(), msgCommittee1))
		require.NoError(t, node1.Broadcast(msgCommittee3.SSVMessage.GetID(), msgCommittee3))
		require.NoError(t, node1.Broadcast(msgProposer.SSVMessage.GetID(), msgProposer))
		require.NoError(t, node1.Broadcast(msgSyncCommitteeContribution.SSVMessage.GetID(), msgSyncCommitteeContribution))
		require.NoError(t, node2.Broadcast(msgRoleVoluntaryExit.SSVMessage.GetID(), msgRoleVoluntaryExit))
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

func generateValidatorMsg(ks *spectestingutils.TestKeySet, round specqbft.Round, nonCommitteeRole spectypes.RunnerRole) *spectypes.SignedSSVMessage {
	if nonCommitteeRole == spectypes.RoleCommittee {
		panic("committee role shouldn't be used here")
	}
	netCfg := networkconfig.TestNetwork
	height := specqbft.Height(netCfg.Beacon.EstimatedCurrentSlot())

	fullData := spectestingutils.TestingQBFTFullData

	nonCommitteeIdentifier := spectypes.NewMsgID(netCfg.DomainType, ks.ValidatorPK.Serialize(), nonCommitteeRole)

	qbftMessage := &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     height,
		Round:      round,
		Identifier: nonCommitteeIdentifier[:],
		Root:       sha256.Sum256(fullData),

		RoundChangeJustification: [][]byte{},
		PrepareJustification:     [][]byte{},
	}

	leader := roundLeader(ks, height, round)
	signedSSVMessage := spectestingutils.SignQBFTMsg(ks.OperatorKeys[leader], leader, qbftMessage)
	signedSSVMessage.FullData = fullData

	return signedSSVMessage
}

func generateCommitteeMsg(ks *spectestingutils.TestKeySet, round specqbft.Round) *spectypes.SignedSSVMessage {
	netCfg := networkconfig.TestNetwork
	height := specqbft.Height(netCfg.Beacon.EstimatedCurrentSlot())

	share := &ssvtypes.SSVShare{
		Share:      *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Status:     eth2apiv1.ValidatorStateActiveOngoing,
		Liquidated: false,
	}
	committeeID := share.CommitteeID()

	fullData := spectestingutils.TestingQBFTFullData

	encodedCommitteeID := append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
	committeeIdentifier := spectypes.NewMsgID(netCfg.DomainType, encodedCommitteeID, spectypes.RoleCommittee)

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

func roundLeader(ks *spectestingutils.TestKeySet, height specqbft.Height, round specqbft.Round) spectypes.OperatorID {
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
	dutyExecutorID := pk
	if role == spectypes.RoleCommittee {
		committeeID := ssvtypes.ComputeCommitteeID([]spectypes.OperatorID{1, 2, 3, 4})
		dutyExecutorID = append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
	}
	id := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, dutyExecutorID, role)

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
			peers := node.PeersByTopic()
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

func Test_score(t *testing.T) {
	const desiredScore = 3

	score0 := score(desiredScore, 0)
	score1 := score(desiredScore, 1)
	score2 := score(desiredScore, 2)
	score3 := score(desiredScore, 3)
	score4 := score(desiredScore, 4)
	score5 := score(desiredScore, 5)
	score6 := score(desiredScore, 6)

	assert.GreaterOrEqual(t, score0, 5*score1)
	assert.GreaterOrEqual(t, score1, 4*score2)
	assert.GreaterOrEqual(t, score2, 3*score3)
	assert.GreaterOrEqual(t, score3, 2*score4)
	assert.GreaterOrEqual(t, score4, score5)
	assert.GreaterOrEqual(t, score5, score6)
}
