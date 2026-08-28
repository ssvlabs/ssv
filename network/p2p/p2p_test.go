package p2pv1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
)

func TestGetMaxPeers(t *testing.T) {
	n := &p2pNetwork{
		cfg: &Config{MaxPeers: 40, TopicMaxPeers: 8},
	}

	require.Equal(t, 40, n.getMaxPeers(""))
	require.Equal(t, 8, n.getMaxPeers("100"))
}

func TestCurrentSubnetsConcurrentAccess(t *testing.T) {
	n := &p2pNetwork{}

	var wg sync.WaitGroup
	start := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := uint64(0); i < 5000; i++ {
			subnets := commons.ZeroSubnets
			subnets.Set(i % commons.SubnetsCount)
			n.setCurrentSubnets(subnets)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 5000; i++ {
			subnets := n.ActiveSubnets()
			_ = subnets.ActiveCount()
			_ = subnets.StringHex()
		}
	}()

	close(start)
	wg.Wait()
}

func TestP2pNetwork_SubscribeBroadcast(t *testing.T) {
	n := 4
	ctx, cancel := context.WithCancel(t.Context())
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
		Shares:       shares,
	})
	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	defer func() {
		for _, node := range ln.Nodes {
			require.NoError(t, node.(*p2pNetwork).Close())
		}
	}()

	// Message validation rejects a committee consensus message whose QBFT round is far from
	// the round it estimates from elapsed slot time (roundBelongsToAllowedSpread), so the
	// committee rounds 1..3 broadcast below are all admitted only within a window of the slot.
	// Broadcasting at an arbitrary wall-clock offset let a run that landed late in a slot get
	// the low rounds rejected, starving the receive-only routers (the ssv-boole-post flake).
	// Broadcast at a fixed point in the slot instead, so the test no longer depends on the
	// wall-clock slot phase.
	const (
		broadcastTimeIntoSlot = 7 * time.Second // fixed point inside the admit-all-of-1..3 window (guarded below)
		minMeshWait           = 3 * time.Second // let the gossip mesh form before broadcasting
	)

	// Guard the choice of broadcastTimeIntoSlot at the source: EstimatedRoundAt is the same
	// estimate the validator uses, so if SlotDuration or the committee head-start ever shifts
	// the estimated round out of the range that admits rounds 1..3, fail loudly here instead
	// of silently reflaking on the router-count assertion below. allowedRoundsInPast and
	// allowedRoundsInFuture are unexported in message/validation; mirror their current values.
	const (
		allowedRoundsInPast   = 2
		allowedRoundsInFuture = 1
	)
	estRound, err := roundtimer.EstimatedRoundAt(spectypes.RoleCommittee, networkconfig.TestNetwork.SlotDuration, broadcastTimeIntoSlot)
	require.NoError(t, err)
	lowestAdmitted := specqbft.FirstRound
	if estRound > allowedRoundsInPast {
		lowestAdmitted = estRound - allowedRoundsInPast
	}
	highestAdmitted := estRound + allowedRoundsInFuture
	require.True(t, lowestAdmitted <= 1 && highestAdmitted >= 3,
		"broadcastTimeIntoSlot=%s puts the estimated committee round at %d (admitted spread [%d, %d]), which does not admit rounds 1..3",
		broadcastTimeIntoSlot, estRound, lowestAdmitted, highestAdmitted)

	// Wait until broadcastTimeIntoSlot into a slot — at least minMeshWait from now so the mesh
	// has formed. Cancellable so a torn-down test returns promptly instead of sleeping for up
	// to a slot.
	wait := broadcastTimeIntoSlot - networkconfig.TestNetwork.EstimatedTimeIntoSlot()
	for wait < minMeshWait {
		wait += networkconfig.TestNetwork.SlotDuration
	}
	select {
	case <-time.After(wait):
	case <-ctx.Done():
		return
	}
	// Log the actual offset so a recurrence (e.g. a scheduling/GC overshoot past the window)
	// is immediately readable instead of surfacing only as the generic router-count failure.
	t.Logf("broadcasting at %s into slot (target %s)", networkconfig.TestNetwork.EstimatedTimeIntoSlot(), broadcastTimeIntoSlot)

	node1, node2 := ln.Nodes[1], ln.Nodes[2]

	var wg sync.WaitGroup
	broadcastErrCh := make(chan error, 6)
	recordBroadcastErr := func(err error) {
		if err != nil {
			broadcastErrCh <- err
		}
	}
	wg.Add(1)

	go func() {
		defer wg.Done()
		msgCommittee1 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 1)
		msgCommittee3 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 3)

		recordBroadcastErr(node1.Broadcast(msgCommittee1.SSVMessage.GetID(), msgCommittee1))
		time.Sleep(time.Millisecond * 20)
		recordBroadcastErr(node2.Broadcast(msgCommittee3.SSVMessage.GetID(), msgCommittee3))
		time.Sleep(time.Millisecond * 20)
		recordBroadcastErr(node2.Broadcast(msgCommittee1.SSVMessage.GetID(), msgCommittee1))
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		msgCommittee1 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 1)
		msgCommittee2 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 2)
		msgCommittee3 := generateCommitteeMsg(spectestingutils.Testing4SharesSet(), 3)

		time.Sleep(time.Millisecond * 20)
		recordBroadcastErr(node1.Broadcast(msgCommittee2.SSVMessage.GetID(), msgCommittee2))

		time.Sleep(time.Millisecond * 20)
		recordBroadcastErr(node2.Broadcast(msgCommittee1.SSVMessage.GetID(), msgCommittee1))
		recordBroadcastErr(node1.Broadcast(msgCommittee3.SSVMessage.GetID(), msgCommittee3))
	}()

	wg.Wait()
	close(broadcastErrCh)
	for err := range broadcastErrCh {
		require.NoError(t, err)
	}

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

func generateCommitteeMsg(ks *spectestingutils.TestKeySet, round specqbft.Round) *spectypes.SignedSSVMessage {
	netCfg := networkconfig.TestNetwork
	height := specqbft.Height(netCfg.EstimatedCurrentSlot())

	share := &ssvtypes.SSVShare{
		Share:      *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
		Status:     eth2apiv1.ValidatorStateActiveOngoing,
		Liquidated: false,
	}
	committeeID := share.CommitteeID()

	fullData := spectestingutils.TestingQBFTFullData

	encodedCommitteeID := append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
	committeeIdentifier := ssvtestingutils.NewMsgID(netCfg.DomainTypeAtSlot(phase0.Slot(height)), encodedCommitteeID, spectypes.RoleCommittee)

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

// roundLeader must agree with the validator's leader check, which delegates to the fork-aware
// qbft.Proposer — the post-fork variant adds an epoch offset that a hand-rolled pre-fork
// formula misses, making every committee proposal reject with ErrSignerNotLeader on the
// post-fork CI leg for ~3 of every 4 epochs.
func roundLeader(ks *spectestingutils.TestKeySet, height specqbft.Height, round specqbft.Round) spectypes.OperatorID {
	share := spectestingutils.TestingShare(ks, 1)

	committee := make([]spectypes.OperatorID, 0, len(share.Committee))
	for _, member := range share.Committee {
		committee = append(committee, member.Signer)
	}

	return qbft.Proposer(height, round, committee, networkconfig.TestNetwork)
}

func dummyMsg(t *testing.T, pkHex string, height int, role spectypes.RunnerRole) (spectypes.MessageID, *spectypes.SignedSSVMessage) {
	pk, err := hex.DecodeString(pkHex)
	require.NoError(t, err)
	dutyExecutorID := pk
	if role == spectypes.RoleCommittee {
		committeeID := ssvtypes.ComputeCommitteeID([]spectypes.OperatorID{1, 2, 3, 4})
		dutyExecutorID = append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
	}
	id := ssvtestingutils.NewMsgID(networkconfig.TestNetwork.DomainTypeAtSlot(phase0.Slot(height)), dutyExecutorID, role)

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
	t.Helper()

	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	ln, err := CreateAndStartLocalNet(ctx, logger.Named("createNetworkAndSubscribe"), options)
	if err != nil {
		return nil, nil, err
	}
	if len(ln.Nodes) != options.Nodes {
		return nil, nil, fmt.Errorf("only %d peers created, expected %d", len(ln.Nodes), options.Nodes)
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

	// Let the nodes subscribe, but fail instead of hanging if the network never converges.
	require.Eventually(t, func() bool {
		for _, node := range ln.Nodes {
			if len(node.PeersByTopic()) < 2 {
				return false
			}
		}
		return true
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for topic peers to connect")

	return ln, routers, nil
}

func TestStartReturnsErrorWhenAlreadyStarted(t *testing.T) {
	n := &p2pNetwork{}
	atomic.StoreInt32(&n.state, stateReady)

	err := n.Start()

	require.Error(t, err)
	require.ErrorContains(t, err, "network already started")
	require.Equal(t, stateReady, atomic.LoadInt32(&n.state), "state should remain stateReady after double-start error")
}
