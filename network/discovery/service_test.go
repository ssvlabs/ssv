package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

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
	assert.NotNil(t, dvs.networkConfig)

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
	updated, err := dvs.RegisterSubnets(testLogger, 1, 3, 5)
	assert.NoError(t, err)
	assert.True(t, updated)

	require.Equal(t, byte(1), dvs.subnets[1])
	require.Equal(t, byte(1), dvs.subnets[3])
	require.Equal(t, byte(1), dvs.subnets[5])
	require.Equal(t, byte(0), dvs.subnets[2])

	// Register the same subnets. Should not update the state
	updated, err = dvs.RegisterSubnets(testLogger, 1, 3, 5)
	assert.NoError(t, err)
	assert.False(t, updated)

	require.Equal(t, byte(1), dvs.subnets[1])
	require.Equal(t, byte(1), dvs.subnets[3])
	require.Equal(t, byte(1), dvs.subnets[5])
	require.Equal(t, byte(0), dvs.subnets[2])

	// Register different subnets
	updated, err = dvs.RegisterSubnets(testLogger, 2, 4)
	assert.NoError(t, err)
	assert.True(t, updated)
	require.Equal(t, byte(1), dvs.subnets[1])
	require.Equal(t, byte(1), dvs.subnets[2])
	require.Equal(t, byte(1), dvs.subnets[3])
	require.Equal(t, byte(1), dvs.subnets[4])
	require.Equal(t, byte(1), dvs.subnets[5])
	require.Equal(t, byte(0), dvs.subnets[6])

	// Close
	err = dvs.Close()
	require.NoError(t, err)
}

func TestDiscV5Service_DeregisterSubnets(t *testing.T) {
	dvs := testingDiscovery(t)

	// Register subnets first
	_, err := dvs.RegisterSubnets(testLogger, 1, 2, 3)
	require.NoError(t, err)

	require.Equal(t, byte(1), dvs.subnets[1])
	require.Equal(t, byte(1), dvs.subnets[2])
	require.Equal(t, byte(1), dvs.subnets[3])

	// Deregister from 2 and 3
	updated, err := dvs.DeregisterSubnets(testLogger, 2, 3)
	assert.NoError(t, err)
	assert.True(t, updated)

	require.Equal(t, byte(1), dvs.subnets[1])
	require.Equal(t, byte(0), dvs.subnets[2])
	require.Equal(t, byte(0), dvs.subnets[3])

	// Deregistering non-existent subnets should not update
	updated, err = dvs.DeregisterSubnets(testLogger, 4, 5)
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
	logger := zap.NewNop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	opts := testingDiscoveryOptions(t, testNetConfig)
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
	dvs.networkConfig = networkconfig.HoleskyStage
	// Test PublishENR method
	dvs.PublishENR(logger)

	// Check LocalNode has been updated
	checkLocalNodeDomainTypeAlignment(t, localNode, networkconfig.HoleskyStage)
}

func TestDiscV5Service_Bootstrap(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	opts := testingDiscoveryOptions(t, testNetConfig)

	dvs, err := newDiscV5Service(testCtx, testLogger, opts)
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
		err := dvs.Bootstrap(logger, handler)
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
	err := dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NewTestingNode(t)))
	require.NoError(t, err)

	// No domain
	err = dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NodeWithoutDomain(t)))
	require.ErrorContains(t, err, "could not read domain type: not found")

	// No next domain. No error since it's not enforced
	err = dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NodeWithoutNextDomain(t)))
	require.NoError(t, err)

	// Matching main domain
	err = dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NodeWithCustomDomains(t, testNetConfig.DomainType, spectypes.DomainType{})))
	require.NoError(t, err)

	// Matching next domain
	err = dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NodeWithCustomDomains(t, spectypes.DomainType{}, testNetConfig.DomainType)))
	require.ErrorContains(t, err, "domain type 00000000 doesn't match 00000302")

	// Mismatching domains
	err = dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NodeWithCustomDomains(t, spectypes.DomainType{}, spectypes.DomainType{})))
	require.ErrorContains(t, err, "domain type 00000000 doesn't match 00000302")

	// No subnets
	err = dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NodeWithoutSubnets(t)))
	require.ErrorContains(t, err, "could not read subnets: not found")

	// Zero subnets
	err = dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NodeWithZeroSubnets(t)))
	require.ErrorContains(t, err, "zero subnets")

	// Valid peer but reached limit
	dvs.conns.(*MockConnection).SetAtLimit(true)
	err = dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NewTestingNode(t)))
	require.ErrorContains(t, err, "reached limit")
	dvs.conns.(*MockConnection).SetAtLimit(false)

	// Valid peer but no common subnet
	subnets := make([]byte, len(commons.ZeroSubnets))
	subnets[10] = 1
	err = dvs.checkPeer(context.TODO(), testLogger, ToPeerEvent(NodeWithCustomSubnets(t, subnets)))
	require.ErrorContains(t, err, "no shared subnets")
}

func TestDiscV5ServiceListenerType(t *testing.T) {

	t.Run("Post-Fork", func(t *testing.T) {
		netConfig := PostForkNetworkConfig()
		dvs := testingDiscoveryWithNetworkConfig(t, netConfig)

		// Check listener type
		_, ok := dvs.dv5Listener.(*forkingDV5Listener)
		require.True(t, ok)

		_, ok = dvs.dv5Listener.(*discover.UDPv5)
		require.False(t, ok)

		// Check bootnodes
		CheckBootnodes(t, dvs, netConfig)

		// Close
		err := dvs.Close()
		require.NoError(t, err)
	})

	t.Run("Pre-Fork", func(t *testing.T) {

		netConfig := PreForkNetworkConfig()
		dvs := testingDiscoveryWithNetworkConfig(t, netConfig)

		// Check listener type
		_, ok := dvs.dv5Listener.(*discover.UDPv5)
		require.False(t, ok)

		_, ok = dvs.dv5Listener.(*forkingDV5Listener)
		require.True(t, ok)

		// Check bootnodes
		CheckBootnodes(t, dvs, netConfig)

		// Close
		err := dvs.Close()
		require.NoError(t, err)
	})
}
