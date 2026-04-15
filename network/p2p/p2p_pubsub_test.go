package p2pv1

import (
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/topics"
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
