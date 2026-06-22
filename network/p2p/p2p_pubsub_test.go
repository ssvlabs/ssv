package p2pv1

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type subscribeRandomsTopicsController struct {
	subscribed []string
}

func (c *subscribeRandomsTopicsController) Subscribe(topic string) error {
	c.subscribed = append(c.subscribed, topic)
	return nil
}

func (c *subscribeRandomsTopicsController) Unsubscribe(string, bool) error {
	return nil
}

func (c *subscribeRandomsTopicsController) Peers(string) ([]peer.ID, error) {
	return nil, nil
}

func (c *subscribeRandomsTopicsController) Topics() []string {
	return nil
}

func (c *subscribeRandomsTopicsController) Broadcast(string, []byte, time.Duration) error {
	return nil
}

func (c *subscribeRandomsTopicsController) UpdateScoreParams() error {
	return nil
}

func (c *subscribeRandomsTopicsController) Close() error {
	return nil
}

var _ topics.Controller = (*subscribeRandomsTopicsController)(nil)

func TestSubscribeRandomsReturnsErrorWhenNotEnoughAvailableSubnets(t *testing.T) {
	currentSubnets := commons.AllSubnets
	currentSubnets.Clear(17)

	topicsCtrl := &subscribeRandomsTopicsController{}
	n := &p2pNetwork{
		state:          stateReady,
		topicsCtrl:     topicsCtrl,
		currentSubnets: currentSubnets,
	}

	err := n.SubscribeRandoms(2)

	require.EqualError(t, err, "not enough available subnets: requested 2, available 1")
	require.Empty(t, topicsCtrl.subscribed)
	require.False(t, n.persistentSubnets.IsSet(17))
}

func TestSubscribeRandomsSubscribesOnlyAvailableSubnets(t *testing.T) {
	currentSubnets := commons.AllSubnets
	currentSubnets.Clear(5)
	currentSubnets.Clear(42)
	currentSubnets.Clear(77)

	topicsCtrl := &subscribeRandomsTopicsController{}
	n := &p2pNetwork{
		state:          stateReady,
		topicsCtrl:     topicsCtrl,
		currentSubnets: currentSubnets,
	}

	err := n.SubscribeRandoms(2)

	require.NoError(t, err)
	require.Len(t, topicsCtrl.subscribed, 2)

	availableSubnets := map[string]struct{}{
		"5":  {},
		"42": {},
		"77": {},
	}
	for _, subnet := range topicsCtrl.subscribed {
		_, ok := availableSubnets[subnet]
		require.Truef(t, ok, "subscribed subnet %s must be chosen from available subnets", subnet)
		delete(availableSubnets, subnet)
	}

	require.Len(t, availableSubnets, 1)
	for subnet := range availableSubnets {
		require.False(t, n.persistentSubnets.IsSet(parseSubnet(t, subnet)))
	}

	for _, subnet := range topicsCtrl.subscribed {
		require.True(t, n.persistentSubnets.IsSet(parseSubnet(t, subnet)))
	}
}

func parseSubnet(t *testing.T, subnet string) uint64 {
	t.Helper()

	value, err := strconv.ParseUint(subnet, 10, 64)
	require.NoError(t, err)

	return value
}

type countingTopicsController struct {
	broadcastCount int
}

func (c *countingTopicsController) Subscribe(string) error { return nil }
func (c *countingTopicsController) Unsubscribe(string, bool) error {
	return nil
}
func (c *countingTopicsController) Peers(string) ([]peer.ID, error) { return nil, nil }
func (c *countingTopicsController) Topics() []string                { return nil }
func (c *countingTopicsController) Broadcast(string, []byte, time.Duration) error {
	c.broadcastCount++
	return errors.New("broadcast should not be called")
}
func (c *countingTopicsController) UpdateScoreParams() error { return nil }
func (c *countingTopicsController) Close() error             { return nil }

var _ topics.Controller = (*countingTopicsController)(nil)

func TestBroadcastSilentMode(t *testing.T) {
	logger := zap.NewNop()
	topicsCtrl := &countingTopicsController{}
	msgID, msg := committeeTestMessage(t)

	n := &p2pNetwork{
		logger:            logger,
		cfg:               &Config{SilentBroadcast: true},
		state:             stateReady,
		topicsCtrl:        topicsCtrl,
		operatorDataStore: operatordatastore.New(&registrystorage.OperatorData{ID: 1}),
	}

	require.NoError(t, n.Broadcast(msgID, msg))
	require.Zero(t, topicsCtrl.broadcastCount)
}

func TestBroadcastSilentModeSkipsNetworkReadyCheck(t *testing.T) {
	logger := zap.NewNop()
	topicsCtrl := &countingTopicsController{}
	msgID, msg := committeeTestMessage(t)

	n := &p2pNetwork{
		logger:            logger,
		cfg:               &Config{SilentBroadcast: true},
		state:             stateClosed,
		topicsCtrl:        topicsCtrl,
		operatorDataStore: operatordatastore.New(&registrystorage.OperatorData{ID: 1}),
	}

	require.NoError(t, n.Broadcast(msgID, msg))
	require.Zero(t, topicsCtrl.broadcastCount)
}

func committeeTestMessage(t *testing.T) (spectypes.MessageID, *spectypes.SignedSSVMessage) {
	t.Helper()

	committeeID := ssvtypes.ComputeCommitteeID([]spectypes.OperatorID{1, 2, 3, 4})
	dutyExecutorID := append(make([]byte, 16), committeeID[:]...)
	msgID := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, dutyExecutorID, spectypes.RoleCommittee)
	qbftMessage := &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     1,
		Round:      2,
		Identifier: msgID[:],
		Root:       [32]byte{0x1, 0x2, 0x3},
	}
	encodedQBFTMessage, err := qbftMessage.Encode()
	require.NoError(t, err)
	return msgID, &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    encodedQBFTMessage,
		},
		Signatures:  [][]byte{[]byte("sig")},
		OperatorIDs: []spectypes.OperatorID{1},
	}
}
