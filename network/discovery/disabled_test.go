package discovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Disabled discovers nothing: Bootstrap never hands out a peer, FindPeers yields a closed channel, the subnet and
// ENR hooks report nothing to update, and it's never stale.
func TestDisabled(t *testing.T) {
	var d Service = Disabled{}

	require.NoError(t, d.Bootstrap(func(PeerEvent) { t.Fatal("no peer is ever discovered") }))

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
	require.False(t, d.DiscoveryStale(time.Minute))
	require.NoError(t, d.Close())
}
