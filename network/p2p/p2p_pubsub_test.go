package p2pv1

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/utils/hashmap"
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

func (c *subscribeRandomsTopicsController) DeregisterTopics(...string) {}

func (c *subscribeRandomsTopicsController) UpdateScoreParams() error {
	return nil
}

func (c *subscribeRandomsTopicsController) Close() error {
	return nil
}

var _ topics.Controller = (*subscribeRandomsTopicsController)(nil)

// subscribeRandomsTestNetCfg returns a NetworkConfig with Boole far enough in the future that
// these tests exercise pre-fork (Alan-only) subscription behavior.
func subscribeRandomsTestNetCfg() *networkconfig.Network {
	cfg := *networkconfig.TestNetwork
	beaconCfg := *networkconfig.TestNetwork.Beacon
	ssvCfg := *networkconfig.TestNetwork.SSV
	ssvCfg.Forks.Boole = cfg.EstimatedCurrentEpoch() + 100
	cfg.Beacon = &beaconCfg
	cfg.SSV = &ssvCfg
	return &cfg
}

func TestSubscribeRandomsReturnsErrorWhenNotEnoughAvailableSubnets(t *testing.T) {
	currentSubnets := commons.AllSubnets
	currentSubnets.Clear(17)

	topicsCtrl := &subscribeRandomsTopicsController{}
	n := &p2pNetwork{
		state:          stateReady,
		cfg:            &Config{NetworkConfig: subscribeRandomsTestNetCfg()},
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
		cfg:            &Config{NetworkConfig: subscribeRandomsTestNetCfg()},
		topicsCtrl:     topicsCtrl,
		currentSubnets: currentSubnets,
	}

	err := n.SubscribeRandoms(2)

	require.NoError(t, err)
	require.Len(t, topicsCtrl.subscribed, 2)

	availableTopics := map[string]uint64{
		commons.GetTopicFullName(commons.SubnetTopicID(5)):  5,
		commons.GetTopicFullName(commons.SubnetTopicID(42)): 42,
		commons.GetTopicFullName(commons.SubnetTopicID(77)): 77,
	}
	for _, topic := range topicsCtrl.subscribed {
		subnet, ok := availableTopics[topic]
		require.Truef(t, ok, "subscribed topic %s must be chosen from available subnets", topic)
		delete(availableTopics, topic)
		require.True(t, n.persistentSubnets.IsSet(subnet))
	}

	require.Len(t, availableTopics, 1)
	for _, subnet := range availableTopics {
		require.False(t, n.persistentSubnets.IsSet(subnet))
	}
}

// TestSubscribedSubnetsForCurrentEpoch covers the Boole transition state machine that decides
// which subnet sets (Alan / Boole) a node subscribes to: Alan-only pre-fork, BOTH during the
// transition window, Boole-only post-fork — for both the node's persistent subnets and its
// active committee subscriptions.
func TestSubscribedSubnetsForCurrentEpoch(t *testing.T) {
	booleCfg := func(booleEpoch phase0.Epoch) *networkconfig.Network {
		cfg := *networkconfig.TestNetwork
		beaconCfg := *networkconfig.TestNetwork.Beacon
		ssvCfg := *networkconfig.TestNetwork.SSV
		ssvCfg.Forks.Boole = booleEpoch
		cfg.Beacon = &beaconCfg
		cfg.SSV = &ssvCfg
		return &cfg
	}

	subnetsOf := func(indices ...uint64) commons.Subnets {
		s := commons.ZeroSubnets
		for _, i := range indices {
			s.Set(i)
		}
		return s
	}

	// A committee subscription contributing Alan subnet 5 / Boole subnet 7.
	newNet := func(cfg *networkconfig.Network) *p2pNetwork {
		n := &p2pNetwork{
			cfg:                  &Config{NetworkConfig: cfg},
			persistentSubnets:    subnetsOf(1, 2),
			subscribedCommittees: hashmap.New[string, statusWithSubnet](),
		}
		n.subscribedCommittees.Set("committee", statusWithSubnet{alanSubnet: 5, booleSubnet: 7})
		return n
	}

	cur := networkconfig.TestNetwork.EstimatedCurrentEpoch()

	t.Run("pre-fork subscribes Alan only", func(t *testing.T) {
		alan, boole := newNet(booleCfg(cur + 100)).subscribedSubnetsForCurrentEpoch()
		require.Equal(t, subnetsOf(1, 2, 5), alan)
		require.Equal(t, commons.ZeroSubnets, boole)
	})

	t.Run("transition window subscribes both", func(t *testing.T) {
		cfg := booleCfg(cur + 1)
		require.True(t, cfg.InBooleTransitionWindow(cfg.EstimatedCurrentSlot()), "expected current slot in the Boole transition window")
		alan, boole := newNet(cfg).subscribedSubnetsForCurrentEpoch()
		require.Equal(t, subnetsOf(1, 2, 5), alan)
		require.Equal(t, subnetsOf(1, 2, 7), boole)
	})

	t.Run("post-fork subscribes Boole only", func(t *testing.T) {
		alan, boole := newNet(booleCfg(0)).subscribedSubnetsForCurrentEpoch()
		require.Equal(t, commons.ZeroSubnets, alan)
		require.Equal(t, subnetsOf(1, 2, 7), boole)
	})
}

// TestBroadcastMessageSlot verifies broadcastMessageSlot resolves the same slot as
// queue.DecodeSignedSSVMessage(msg).Slot() for every message type Broadcast may see, without
// building the full queue.DecodedSSVMessage wrapper.
func TestBroadcastMessageSlot(t *testing.T) {
	t.Run("consensus message", func(t *testing.T) {
		qbftMsg := &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     123,
			Round:      1,
			Identifier: make([]byte, 56),
			Root:       [32]byte{1, 2, 3},
		}
		data, err := qbftMsg.Encode()
		require.NoError(t, err)

		msg := &spectypes.SignedSSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.MessageID(qbftMsg.Identifier),
				Data:    data,
			},
		}

		slot, err := broadcastMessageSlot(msg)
		require.NoError(t, err)
		require.Equal(t, phase0.Slot(123), slot)

		decoded, err := queue.DecodeSignedSSVMessage(msg)
		require.NoError(t, err)
		wantSlot, err := decoded.Slot()
		require.NoError(t, err)
		require.Equal(t, wantSlot, slot)
	})

	t.Run("partial signature message", func(t *testing.T) {
		partialMsg := &spectypes.PartialSignatureMessages{
			Type: spectypes.PostConsensusPartialSig,
			Slot: 456,
			Messages: []*spectypes.PartialSignatureMessage{{
				PartialSignature: make([]byte, 96),
				SigningRoot:      [32]byte{},
				Signer:           1,
				ValidatorIndex:   1,
			}},
		}
		data, err := partialMsg.Encode()
		require.NoError(t, err)

		msg := &spectypes.SignedSSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   spectypes.MessageID(make([]byte, 56)),
				Data:    data,
			},
		}

		slot, err := broadcastMessageSlot(msg)
		require.NoError(t, err)
		require.Equal(t, phase0.Slot(456), slot)

		decoded, err := queue.DecodeSignedSSVMessage(msg)
		require.NoError(t, err)
		wantSlot, err := decoded.Slot()
		require.NoError(t, err)
		require.Equal(t, wantSlot, slot)
	})

	t.Run("unknown message type returns error matching decode path", func(t *testing.T) {
		msg := &spectypes.SignedSSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: 99,
				MsgID:   spectypes.MessageID(make([]byte, 56)),
				Data:    []byte{1},
			},
		}

		_, err := broadcastMessageSlot(msg)
		require.ErrorIs(t, err, queue.ErrUnknownMessageType)

		_, decodeErr := queue.DecodeSignedSSVMessage(msg)
		require.ErrorIs(t, decodeErr, queue.ErrUnknownMessageType)
	})

	t.Run("invalid consensus message data returns error", func(t *testing.T) {
		msg := &spectypes.SignedSSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.MessageID(make([]byte, 56)),
				Data:    []byte{1, 2, 3},
			},
		}

		_, err := broadcastMessageSlot(msg)
		require.Error(t, err)

		_, decodeErr := queue.DecodeSignedSSVMessage(msg)
		require.Error(t, decodeErr)
	})
}
