package discovery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDisabled checks that Disabled discovers nothing: Bootstrap never hands out a peer, FindPeers yields a closed
// channel, and the subnet and ENR hooks report nothing to update.
func TestDisabled(t *testing.T) {
	var d Service = Disabled{}

	var handlerCalls int
	require.NoError(t, d.Bootstrap(func(PeerEvent) { handlerCalls++ }))
	require.Zero(t, handlerCalls, "Bootstrap must never hand a peer to the handler")

	peers, err := d.FindPeers(t.Context(), "ns")
	require.NoError(t, err)
	_, open := <-peers
	require.False(t, open, "the peer channel is closed without a peer on it")

	ttl, err := d.Advertise(t.Context(), "ns")
	require.NoError(t, err)
	require.Zero(t, ttl)

	updated, err := d.RegisterSubnets(1, 2)
	require.NoError(t, err)
	require.False(t, updated)
	updated, err = d.DeregisterSubnets(1, 2)
	require.NoError(t, err)
	require.False(t, updated)

	d.PublishENR()
	require.NoError(t, d.Close())
}
