package p2pv1

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/networkconfig"
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
