package p2pv1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/herumi/bls-eth-go-binary/bls"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
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
	const numNodes = 4
	const numKeySets = 3
	const broadcastDelay = 20 * time.Millisecond
	const waitTimeout = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	validatorKeySets, err := generateValidatorKeySets(numKeySets)
	require.NoError(t, err)

	// Generate and assign shares
	shares := generateShares(t, numKeySets)
	for i, share := range shares {
		share.Share = *spectestingutils.TestingShare(validatorKeySets[i], share.ValidatorIndex)
	}

	for i, share := range shares {
		share.Share = *spectestingutils.TestingShare(validatorKeySets[i], share.ValidatorIndex)
	}

	ln, routers, err := createNetworkAndSubscribe(t, ctx, LocalNetOptions{
		Nodes:        numNodes,
		MinConnected: numNodes/2 - 1,
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
		msg1 := generateMsg(validatorKeySets[0], 1)
		msg2 := generateMsg(validatorKeySets[1], 2)
		msg3 := generateMsg(validatorKeySets[2], 3)
		require.NoError(t, node1.Broadcast(msg1.SSVMessage.GetID(), msg1))
		<-time.After(broadcastDelay)
		require.NoError(t, node2.Broadcast(msg2.SSVMessage.GetID(), msg3))
		<-time.After(broadcastDelay)
		require.NoError(t, node2.Broadcast(msg3.SSVMessage.GetID(), msg1))
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		msg1 := generateMsg(validatorKeySets[0], 1)
		msg2 := generateMsg(validatorKeySets[1], 2)
		msg3 := generateMsg(validatorKeySets[2], 3)
		require.NoError(t, err)

		time.Sleep(broadcastDelay)
		require.NoError(t, node1.Broadcast(msg2.SSVMessage.GetID(), msg2))

		time.Sleep(broadcastDelay)
		require.NoError(t, node2.Broadcast(msg1.SSVMessage.GetID(), msg1))
		require.NoError(t, node1.Broadcast(msg3.SSVMessage.GetID(), msg3))
	}()

	wg.Wait()

	// waiting for messages
	wg.Add(1)
	go func() {
		ct, cancel := context.WithTimeout(ctx, waitTimeout)
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
func generateKeySetsWithRandomPK() (*spectestingutils.TestKeySet, error) {
	sharesSets := spectestingutils.Testing4SharesSet()
	sk, pk, err := generateSKAndPK()
	if err != nil {
		return nil, err
	}
	sharesSets.ValidatorPK = pk
	sharesSets.ValidatorSK = sk
	return sharesSets, nil
}

func generateSKAndPK() (*bls.SecretKey, *bls.PublicKey, error) {
	// Initialize the BLS library
	bls.Init(bls.BLS12_381)

	// Generate a new secret key
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG() // Uses cryptographically secure random number generation

	// Derive the public key from the secret key
	pk := sk.GetPublicKey()

	return sk, pk, nil
}

func generateValidatorKeySets(numKeySets int) ([]*spectestingutils.TestKeySet, error) {
	validatorKeySets := make([]*spectestingutils.TestKeySet, 0, numKeySets)
	for i := 0; i < numKeySets; i++ {
		keySet, err := generateKeySetsWithRandomPK()
		if err != nil {
			return nil, fmt.Errorf("error generating key set %d: %w", i+1, err)
		}
		validatorKeySets = append(validatorKeySets, keySet)
	}
	return validatorKeySets, nil
}
