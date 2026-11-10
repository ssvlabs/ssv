package commons

import (
	crand "crypto/rand"
	"fmt"
	"net"
	"slices"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func Test_BuildMultiAddress(t *testing.T) {
	sk, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)

	t.Run("IPv4", func(t *testing.T) {
		ma, err := BuildMultiAddress(DefaultIP, "tcp", DefaultTCP, id)
		require.NoError(t, err)
		expected := fmt.Sprintf("/ip4/%s/%s/%d/p2p/%s", DefaultIP, "tcp", DefaultTCP, id.String())
		require.Equal(t, expected, ma.String())
	})

	t.Run("IPv6", func(t *testing.T) {
		ma, err := BuildMultiAddress(net.IPv6zero.String(), "tcp", DefaultTCP, id)
		require.NoError(t, err)
		expected := fmt.Sprintf("/ip6/%s/%s/%d/p2p/%s", net.IPv6zero.String(), "tcp", DefaultTCP, id.String())
		require.Equal(t, expected, ma.String())
	})
}

// TestIPAddr checks that IPAddr returns one of this machine's addresses, preferring the default route's.
func TestIPAddr(t *testing.T) {
	ip, err := IPAddr()
	require.NoError(t, err)

	addrs, err := net.InterfaceAddrs()
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(addrs, func(addr net.Addr) bool {
		ipNet, ok := addr.(*net.IPNet)
		return ok && ipNet.IP.Equal(ip)
	}), "%s isn't one of this machine's addresses", ip)

	if routeIP := defaultRouteIPv4(); routeIP != nil {
		require.True(t, routeIP.Equal(ip), "got %s, want the default route's %s", ip, routeIP)
	}
}
