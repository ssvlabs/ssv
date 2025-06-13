package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/networkconfig"
)

func CheckBootnodes(t *testing.T, dvs *DiscV5Service, netConfig networkconfig.NetworkConfig) {

	require.Len(t, dvs.bootnodes, len(netConfig.Bootnodes))

	for _, bootnode := range netConfig.Bootnodes {
		nodes, err := ParseENR(nil, false, bootnode)
		require.NoError(t, err)
		require.Contains(t, dvs.bootnodes, nodes[0])
	}
}

func TestNewDiscV5Service(t *testing.T) {
	dvs := testingDiscovery(t)

	assert.NotNil(t, dvs.dv5Listener)
	assert.NotNil(t, dvs.conns)
	assert.NotNil(t, dvs.subnetsIdx)
	assert.NotNil(t, dvs.ssvConfig)

	// Check bootnodes
	CheckBootnodes(t, dvs, testNetConfig)

	// Close
	err := dvs.Close()
	require.NoError(t, err)
}

func TestDiscV5Service_Close(t *testing.T) {
	dvs := testingDiscovery(t)

	err := dvs.Close()
	assert.NoError(t, err)
}

func TestDiscV5Service_RegisterSubnets(t *testing.T) {
	dvs := testingDiscovery(t)

	// Register subnets 1, 3, and 5
	updated, err := dvs.RegisterSubnets(1, 3, 5)
	assert.NoError(t, err)
	assert.True(t, updated)

	require.True(t, dvs.subnets.IsSet(1))
	require.True(t, dvs.subnets.IsSet(3))
	require.True(t, dvs.subnets.IsSet(5))
	require.False(t, dvs.subnets.IsSet(2))

	// Register the same subnets. Should not update the state
	updated, err = dvs.RegisterSubnets(1, 3, 5)
	assert.NoError(t, err)
	assert.False(t, updated)

	require.True(t, dvs.subnets.IsSet(1))
	require.True(t, dvs.subnets.IsSet(3))
	require.True(t, dvs.subnets.IsSet(5))
	require.False(t, dvs.subnets.IsSet(2))

	// Register different subnets
	updated, err = dvs.RegisterSubnets(2, 4)
	assert.NoError(t, err)
	assert.True(t, updated)
	require.True(t, dvs.subnets.IsSet(1))
	require.True(t, dvs.subnets.IsSet(2))
	require.True(t, dvs.subnets.IsSet(3))
	require.True(t, dvs.subnets.IsSet(4))
	require.True(t, dvs.subnets.IsSet(5))
	require.False(t, dvs.subnets.IsSet(6))

	// Close
	err = dvs.Close()
	require.NoError(t, err)
}

func TestDiscV5Service_DeregisterSubnets(t *testing.T) {
	dvs := testingDiscovery(t)

	// Register subnets first
	_, err := dvs.RegisterSubnets(1, 2, 3)
	require.NoError(t, err)

	require.True(t, dvs.subnets.IsSet(1))
	require.True(t, dvs.subnets.IsSet(2))
	require.True(t, dvs.subnets.IsSet(3))

	// Deregister from 2 and 3
	updated, err := dvs.DeregisterSubnets(2, 3)
	assert.NoError(t, err)
	assert.True(t, updated)

	require.True(t, dvs.subnets.IsSet(1))
	require.False(t, dvs.subnets.IsSet(2))
	require.False(t, dvs.subnets.IsSet(3))

	// Deregistering non-existent subnets should not update
	updated, err = dvs.DeregisterSubnets(4, 5)
	assert.NoError(t, err)
	assert.False(t, updated)

	// Close
	err = dvs.Close()
	require.NoError(t, err)
}

func checkLocalNodeDomainTypeAlignment(t *testing.T, localNode *enode.LocalNode, netConfig networkconfig.NetworkConfig) {
	// Check domain entry
	domainEntry := records.DomainTypeEntry{
		Key:        records.KeyDomainType,
		DomainType: spectypes.DomainType{},
	}
	err := localNode.Node().Record().Load(&domainEntry)
	require.NoError(t, err)
	require.Equal(t, netConfig.DomainType, domainEntry.DomainType)

	// Check next domain entry
	nextDomainEntry := records.DomainTypeEntry{
		Key:        records.KeyNextDomainType,
		DomainType: spectypes.DomainType{},
	}
	err = localNode.Node().Record().Load(&nextDomainEntry)
	require.NoError(t, err)
	require.Equal(t, netConfig.DomainType, nextDomainEntry.DomainType)
}

func TestDiscV5Service_PublishENR(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	opts := testingDiscoveryOptions(t, testNetConfig.SSVConfig)
	dvs, err := newDiscV5Service(ctx, testLogger, opts)
	require.NoError(t, err)

	// Replace listener
	localNode := dvs.Self()
	err = dvs.Close()
	require.NoError(t, err)
	dvs.dv5Listener = NewMockListener(localNode, []*enode.Node{NewTestingNode(t)})

	// Check LocalNode has the correct domain and next domain entries
	checkLocalNodeDomainTypeAlignment(t, localNode, testNetConfig)

	// Change network config
	dvs.ssvConfig = networkconfig.TestNetwork.SSVConfig
	// Test PublishENR method
	dvs.PublishENR()

	// Check LocalNode has been updated
	checkLocalNodeDomainTypeAlignment(t, localNode, networkconfig.TestNetwork)
}

func TestDiscV5Service_Bootstrap(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	opts := testingDiscoveryOptions(t, testNetConfig.SSVConfig)

	dvs, err := newDiscV5Service(t.Context(), testLogger, opts)
	require.NoError(t, err)

	// Replace listener
	err = dvs.conn.Close()
	require.NoError(t, err)
	testingNode := NewTestingNode(t)
	dvs.dv5Listener = NewMockListener(dvs.Self(), []*enode.Node{testingNode})

	// testing handler. It's called whenever a new peer is found
	handlerCalled := make(chan struct{})
	handler := func(e PeerEvent) {
		require.Equal(t, testingNode, e.Node)
		close(handlerCalled)
	}

	// Run bootstrap
	go func() {
		err := dvs.Bootstrap(handler)
		assert.NoError(t, err)
	}()

	// Wait for testing peer to be found
	select {
	case <-handlerCalled:
		// Test passed
	case <-ctx.Done():
		t.Fatal("Bootstrap timed out")
	}
}

func TestDiscV5Service_Node(t *testing.T) {
	dvs := testingDiscovery(t)

	// Replace listener
	err := dvs.conn.Close()
	require.NoError(t, err)
	testingNode := NewTestingNode(t)
	dvs.dv5Listener = NewMockListener(dvs.Self(), []*enode.Node{testingNode})

	// Create a mock peer.AddrInfo
	unknownPeer := NewTestingNode(t)
	unknownPeerAddrInfo, err := ToPeer(unknownPeer)
	assert.NoError(t, err)

	// Test looking for an unknown peer
	node, err := dvs.Node(testLogger, *unknownPeerAddrInfo)
	assert.NoError(t, err)
	assert.Nil(t, node)

	// Test looking for a known peer
	addrInfo, err := ToPeer(testingNode)
	assert.NoError(t, err)
	node, err = dvs.Node(testLogger, *addrInfo)
	assert.NoError(t, err)
	assert.Equal(t, testingNode, node)
}

func TestDiscV5Service_checkPeer(t *testing.T) {
	dvs := testingDiscovery(t)

	defer func() {
		err := dvs.conn.Close()
		require.NoError(t, err)
	}()

	// Valid peer
	err := dvs.checkPeer(context.TODO(), ToPeerEvent(NewTestingNode(t)))
	require.NoError(t, err)

	// No domain
	err = dvs.checkPeer(context.TODO(), ToPeerEvent(NodeWithoutDomain(t)))
	require.ErrorContains(t, err, "could not read domain type: not found")

	// No next domain. No error since it's not enforced
	err = dvs.checkPeer(context.TODO(), ToPeerEvent(NodeWithoutNextDomain(t)))
	require.NoError(t, err)

	// Matching main domain
	err = dvs.checkPeer(context.TODO(), ToPeerEvent(NodeWithCustomDomains(t, testNetConfig.DomainType, spectypes.DomainType{})))
	require.NoError(t, err)

	// Matching next domain
	err = dvs.checkPeer(context.TODO(), ToPeerEvent(NodeWithCustomDomains(t, spectypes.DomainType{}, testNetConfig.DomainType)))
	require.ErrorContains(t, err, "domain type 00000000 doesn't match 00000302")

	// Mismatching domains
	err = dvs.checkPeer(context.TODO(), ToPeerEvent(NodeWithCustomDomains(t, spectypes.DomainType{}, spectypes.DomainType{})))
	require.ErrorContains(t, err, "domain type 00000000 doesn't match 00000302")

	// No subnets
	err = dvs.checkPeer(context.TODO(), ToPeerEvent(NodeWithoutSubnets(t)))
	require.ErrorContains(t, err, "could not read subnets: not found")

	// Zero subnets
	err = dvs.checkPeer(context.TODO(), ToPeerEvent(NodeWithZeroSubnets(t)))
	require.ErrorContains(t, err, "zero subnets")

	// Valid peer but reached limit
	dvs.conns.(*MockConnection).SetAtLimit(true)
	err = dvs.checkPeer(context.TODO(), ToPeerEvent(NewTestingNode(t)))
	require.ErrorContains(t, err, "reached limit")
	dvs.conns.(*MockConnection).SetAtLimit(false)

	// Valid peer but no common subnet
	subnets := commons.Subnets{}
	subnets.Set(10)
	err = dvs.checkPeer(context.TODO(), ToPeerEvent(NodeWithCustomSubnets(t, subnets)))
	require.ErrorContains(t, err, "no shared subnets")
}

func TestDiscV5ServiceListenerType(t *testing.T) {
	dvs := testingDiscovery(t)

	// Check listener type
	_, ok := dvs.dv5Listener.(*discover.UDPv5)
	require.False(t, ok)

	_, ok = dvs.dv5Listener.(*forkingDV5Listener)
	require.True(t, ok)

	// Check bootnodes
	CheckBootnodes(t, dvs, testNetConfig)

	// Close
	err := dvs.Close()
	require.NoError(t, err)
}

// TestServiceAddressConfiguration tests the address configuration logic in the discovery service.
// It verifies that host addresses and DNS names are correctly resolved and applied
// to the local node, including fallback behavior when DNS resolution fails.
func TestServiceAddressConfiguration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		hostAddress    string
		hostDNS        string
		expectedResult string // if result is deterministic; for "localhost" we can't always predict exact string
		expectError    bool
		checkLoopback  bool // if true, result should be a loopback address
	}{
		{
			name:           "only HostAddress",
			hostAddress:    "192.168.1.100",
			hostDNS:        "",
			expectedResult: "192.168.1.100",
			expectError:    false,
		},
		{
			name:          "only HostDNS",
			hostAddress:   "",
			hostDNS:       "localhost",
			expectError:   false,
			checkLoopback: true, // expect "localhost" to resolve to a loopback address
		},
		{
			name:          "both HostAddress and HostDNS",
			hostAddress:   "192.168.1.100",
			hostDNS:       "localhost",
			expectError:   false,
			checkLoopback: true, // DNS takes precedence over static address
		},
		{
			name:        "non-resolvable HostDNS with fallback to HostAddress",
			hostAddress: "192.168.1.100",
			hostDNS:     "nonexistent-domain-qwerty.local",
			expectError: true,
		},
		{
			name:           "both empty",
			hostAddress:    "",
			hostDNS:        "",
			expectedResult: "",
			expectError:    false,
		},
		{
			name:        "invalid host address format",
			hostAddress: "not-an-ip-address",
			hostDNS:     "",
			expectError: true,
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			// create options with unique ports for parallel testing
			opts := testingDiscoveryOptions(t, testNetConfig.SSVConfig)
			opts.DiscV5Opts.Port = uint16(13000 + i*10)
			opts.DiscV5Opts.TCPPort = uint16(14000 + i*10)
			opts.HostAddress = tc.hostAddress
			opts.HostDNS = tc.hostDNS

			service, err := NewService(ctx, zap.NewNop(), *opts)
			if tc.expectError {
				require.Error(t, err)

				// for invalid address test case, check for the specific error message
				if tc.name == "invalid host address format" {
					require.ErrorContains(t, err, "invalid host address given")
				}
				return
			}
			require.NoError(t, err)

			t.Cleanup(func() {
				if service != nil {
					service.Close()
				}
			})

			// verify we got the expected service type
			dv5Service, ok := service.(*DiscV5Service)
			require.True(t, ok)

			// check that the node has the expected IP configuration
			localNode := dv5Service.Self()
			ip := localNode.Node().IP()

			if tc.expectedResult != "" {
				require.Equal(t, tc.expectedResult, ip.String())
			}

			// for "localhost" we can't always predict the exact string
			// but we can check if it's a loopback address
			if tc.checkLoopback {
				require.True(t, ip.IsLoopback())
			}
		})
	}
}
