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

// TestIPAddr checks that IPAddr returns one of this machine's addresses.
func TestIPAddr(t *testing.T) {
	ip, err := IPAddr()
	require.NoError(t, err)
	requireMachineAddr(t, ip)
}

// Test_pickIP checks that pickIP returns a route source peers could reach us at, private or public, and falls back
// to one of this machine's addresses for any other.
func Test_pickIP(t *testing.T) {
	for _, routeSource := range []string{"10.0.1.5", "203.0.113.7"} {
		ip, err := pickIP(net.ParseIP(routeSource))
		require.NoError(t, err)
		require.Equal(t, routeSource, ip.String())
	}

	unusable := map[string]net.IP{
		"none":        nil,
		"unspecified": net.IPv4zero,
		"loopback":    net.IPv4(127, 0, 0, 1),
		"link-local":  net.IPv4(169, 254, 1, 1),
		"multicast":   net.IPv4(224, 0, 0, 1),
		"IPv6":        net.ParseIP("2001:db8::1"),
	}
	for name, routeSource := range unusable {
		t.Run(name, func(t *testing.T) {
			ip, err := pickIP(routeSource)
			require.NoError(t, err)
			requireMachineAddr(t, ip)
		})
	}
}

// requireMachineAddr fails the test unless ip is one of this machine's addresses.
func requireMachineAddr(t *testing.T, ip net.IP) {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(addrs, func(addr net.Addr) bool {
		ipNet, ok := addr.(*net.IPNet)
		return ok && ipNet.IP.Equal(ip)
	}), "%s isn't one of this machine's addresses", ip)
}
