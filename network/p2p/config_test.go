package p2pv1

import (
	"testing"

	"github.com/libp2p/go-libp2p"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

// TestConfig_ApplyDefaults checks ApplyDefaults seeds the in-code p2p defaults, spot-checking the
// true-default bools that motivated moving defaults out of cleanenv env-default tags (#2868): a
// bool's zero value is false, so env-default:"true" couldn't be told apart from an explicit false.
func TestConfig_ApplyDefaults(t *testing.T) {
	var c Config
	c.ApplyDefaults()
	require.True(t, c.DynamicMaxPeers)
	require.True(t, c.PubSubScoring)
	require.Equal(t, 60, c.MaxPeers)
	require.Equal(t, "discv5", c.Discovery)
}

// TestConfig_AddrsFactory checks that with HostAddress or HostDNS set, the host advertises that
// address plus the public ones among the addresses go-libp2p resolves 0.0.0.0 to, and none of the
// private ones. Without either, no factory is set and libp2p advertises everything as before.
func TestConfig_AddrsFactory(t *testing.T) {
	resolved := []ma.Multiaddr{
		ma.StringCast("/ip4/127.0.0.1/tcp/13001"),
		ma.StringCast("/ip4/10.0.1.5/tcp/13001"),
		ma.StringCast("/ip4/172.17.0.1/tcp/13001"),      // docker bridge
		ma.StringCast("/ip4/100.101.102.103/tcp/13001"), // CGNAT range, e.g. a VPN
		ma.StringCast("/ip4/1.2.3.4/tcp/13001"),
	}

	tests := []struct {
		name        string
		hostAddress string
		hostDNS     string
		want        []string // nil when no factory is expected
	}{
		{
			name: "no external address",
		},
		{
			name:        "host address",
			hostAddress: "5.6.7.8",
			want:        []string{"/ip4/1.2.3.4/tcp/13001", "/ip4/5.6.7.8/tcp/13001"},
		},
		{
			name:        "private host address",
			hostAddress: "10.0.1.5",
			want:        []string{"/ip4/1.2.3.4/tcp/13001", "/ip4/10.0.1.5/tcp/13001"},
		},
		{
			name:    "host DNS",
			hostDNS: "ssv.example.com",
			want:    []string{"/ip4/1.2.3.4/tcp/13001", "/dns4/ssv.example.com/tcp/13001"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{TCPPort: 13001, HostAddress: tt.hostAddress, HostDNS: tt.hostDNS}
			opts, err := c.configureAddrs(nil)
			require.NoError(t, err)

			var hostCfg libp2p.Config
			require.NoError(t, hostCfg.Apply(opts...))
			if tt.want == nil {
				require.Nil(t, hostCfg.AddrsFactory)
				return
			}
			require.NotNil(t, hostCfg.AddrsFactory)

			var advertised []string
			for _, addr := range hostCfg.AddrsFactory(resolved) {
				advertised = append(advertised, addr.String())
			}
			require.Equal(t, tt.want, advertised)
		})
	}
}

// TestConfig_InvalidExternalAddress checks that a HostAddress or HostDNS that doesn't make a valid
// multiaddr fails the libp2p setup, rather than leaving the host to advertise no external address.
func TestConfig_InvalidExternalAddress(t *testing.T) {
	c := Config{TCPPort: 13001, HostAddress: "not-an-ip"}
	_, err := c.configureAddrs(nil)
	require.ErrorContains(t, err, "could not build external multi address")

	c = Config{TCPPort: 13001, HostDNS: "not/a/name"}
	_, err = c.configureAddrs(nil)
	require.ErrorContains(t, err, "could not build external multi address")
}
