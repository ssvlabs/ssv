package p2pv1

import (
	"fmt"
	"net"
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	p2ptesting "github.com/ssvlabs/ssv/network/testing"
	"github.com/ssvlabs/ssv/networkconfig"
)

// TestSetupDiscovery checks that setupDiscovery installs the service the discovery mode names, runs discv5 for an
// unset mode, and rejects an unknown mode instead of falling back to discv5.
func TestSetupDiscovery(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		wantType string // %T of the installed service: mdns's type is unexported, so it can't be named directly
		wantErr  string
	}{
		{name: "discv5", mode: discv5Discovery, wantType: "*discovery.DiscV5Service"},
		{name: "unset", mode: "", wantType: "*discovery.DiscV5Service"},
		{name: "mdns", mode: mdnsDiscovery, wantType: "*discovery.localDiscovery"},
		{name: "none", mode: noDiscovery, wantType: "discovery.Disabled"},
		{name: "unknown", mode: "nome", wantErr: `unknown discovery mode "nome"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := newDiscoveryTestNetwork(t, tt.mode)

			err := n.setupDiscovery()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, n.disc, "an unknown mode must not install a service")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantType, fmt.Sprintf("%T", n.disc))
		})
	}
}

// newDiscoveryTestNetwork returns a network with what setupDiscovery needs for any mode: a host, a network key, a
// free UDP port for discv5, and no bootnodes, so discv5 contacts nothing outside the test. Its cleanup closes the
// network, and with it the host and the installed service.
func newDiscoveryTestNetwork(t *testing.T, mode string) *p2pNetwork {
	netKey, err := p2ptesting.GenNetworkKey()
	require.NoError(t, err)

	ssvCfg := *networkconfig.TestNetwork.SSV
	ssvCfg.Bootnodes = nil

	n, err := New(zap.NewNop(), &Config{
		Ctx:               t.Context(),
		Discovery:         mode,
		NetworkPrivateKey: netKey,
		UDPPort:           freeUDPPort(t),
		NetworkConfig:     &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &ssvCfg},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, n.Close()) })

	h, err := libp2p.New(libp2p.NoListenAddrs)
	require.NoError(t, err)
	n.host.Store(&h)

	return n
}

// freeUDPPort returns a UDP port free at the time of the call: discv5 rejects port 0, so the kernel can't pick one
// at bind time.
func freeUDPPort(t *testing.T) uint16 {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	return uint16(conn.LocalAddr().(*net.UDPAddr).Port)
}
