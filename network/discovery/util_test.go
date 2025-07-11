package discovery

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils/ttl"
)

var (
	testLogger    = zap.NewNop()
	testNetConfig = networkconfig.TestNetwork

	testIP             = "127.0.0.1"
	testBindIP         = "127.0.0.1"
	testPort    uint16 = 12001
	testTCPPort uint16 = 13001
)

// Options for the discovery service
func testingDiscoveryOptions(t *testing.T, ssvConfig *networkconfig.SSV) *Options {
	// Generate key
	privKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	// Discv5 options
	discV5Opts := &DiscV5Options{
		StoragePath: t.TempDir(),
		IP:          testIP,
		BindIP:      testBindIP,

		Port:          testPort,
		TCPPort:       testTCPPort,
		NetworkKey:    privKey,
		Bootnodes:     ssvConfig.Bootnodes,
		Subnets:       mockSubnets(1),
		EnableLogging: false,
	}

	// Discovery options
	subnetsIndex := peers.NewSubnetsIndex()
	connectionIndex := NewMockConnection()

	return &Options{
		DiscV5Opts:          discV5Opts,
		ConnIndex:           connectionIndex,
		SubnetsIdx:          subnetsIndex,
		SSVConfig:           ssvConfig,
		DiscoveredPeersPool: ttl.New[peer.ID, DiscoveredPeer](time.Hour, time.Hour),
		TrimmedRecently:     ttl.New[peer.ID, struct{}](time.Hour, time.Hour),
	}
}

// Testing discovery with a given Network
func testingDiscoveryWithNetworkConfig(t *testing.T, ssvConfig *networkconfig.SSV) *DiscV5Service {
	opts := testingDiscoveryOptions(t, ssvConfig)
	dvs, err := newDiscV5Service(t.Context(), testLogger, opts)
	require.NoError(t, err)
	require.NotNil(t, dvs)
	return dvs
}

// Testing discovery service
func testingDiscovery(t *testing.T) *DiscV5Service {
	return testingDiscoveryWithNetworkConfig(t, testNetConfig.SSV)
}

// Testing LocalNode
func NewLocalNode(t *testing.T) *enode.LocalNode {
	// Generate key
	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create local node
	localNode, err := records.CreateLocalNode(nodeKey, t.TempDir(), net.IP(testIP), testPort, testTCPPort)
	require.NoError(t, err)

	// Set entries
	err = records.SetDomainTypeEntry(localNode, records.KeyDomainType, testNetConfig.DomainType)
	require.NoError(t, err)
	err = records.SetDomainTypeEntry(localNode, records.KeyNextDomainType, testNetConfig.DomainType)
	require.NoError(t, err)
	err = records.SetSubnetsEntry(localNode, mockSubnets(1))
	require.NoError(t, err)

	return localNode
}

// Testing node
func NewTestingNode(t *testing.T) *enode.Node {
	return CustomNode(t, true, testNetConfig.DomainType, true, testNetConfig.DomainType, true, mockSubnets(1))
}

func NewTestingNodes(t *testing.T, count int) []*enode.Node {
	nodes := make([]*enode.Node, count)
	for i := 0; i < count; i++ {
		nodes[i] = NewTestingNode(t)
	}
	return nodes
}

func NodeWithoutDomain(t *testing.T) *enode.Node {
	return CustomNode(t, false, spectypes.DomainType{}, true, testNetConfig.DomainType, true, mockSubnets(1))
}

func NodeWithoutNextDomain(t *testing.T) *enode.Node {
	return CustomNode(t, true, testNetConfig.DomainType, false, spectypes.DomainType{}, true, mockSubnets(1))
}

func NodeWithoutSubnets(t *testing.T) *enode.Node {
	return CustomNode(t, true, testNetConfig.DomainType, true, testNetConfig.DomainType, false, commons.Subnets{})
}

func NodeWithCustomDomains(t *testing.T, domainType spectypes.DomainType, nextDomainType spectypes.DomainType) *enode.Node {
	return CustomNode(t, true, domainType, true, nextDomainType, true, mockSubnets(1))
}

func NodeWithZeroSubnets(t *testing.T) *enode.Node {
	return CustomNode(t, true, testNetConfig.DomainType, true, testNetConfig.DomainType, true, commons.ZeroSubnets)
}

func NodeWithCustomSubnets(t *testing.T, subnets commons.Subnets) *enode.Node {
	return CustomNode(t, true, testNetConfig.DomainType, true, testNetConfig.DomainType, true, subnets)
}

func CustomNode(t *testing.T,
	setDomainType bool, domainType spectypes.DomainType,
	setNextDomainType bool, nextDomainType spectypes.DomainType,
	setSubnets bool, subnets commons.Subnets) *enode.Node {

	// Generate key
	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Encoding and decoding (hack so that SignV4 works)
	hexPrivKey := hex.EncodeToString(crypto.FromECDSA(nodeKey))
	sk, err := crypto.HexToECDSA(hexPrivKey)
	require.NoError(t, err)

	// Create record
	record := enr.Record{}

	// Set entries
	record.Set(enr.WithEntry("ssv", true)) // marks node as SSV-related (we filter out SSV-unrelated ones)
	record.Set(enr.IP(net.IPv4(127, 0, 0, 1)))
	record.Set(enr.UDP(12000))
	record.Set(enr.TCP(13000))
	if setDomainType {
		record.Set(records.DomainTypeEntry{
			Key:        records.KeyDomainType,
			DomainType: domainType,
		})
	}
	if setNextDomainType {
		record.Set(records.DomainTypeEntry{
			Key:        records.KeyNextDomainType,
			DomainType: nextDomainType,
		})
	}
	if setSubnets {
		subnetsVec := bitfield.NewBitvector128()
		for i := uint64(0); i < commons.SubnetsCount; i++ {
			subnetsVec.SetBitAt(i, subnets.IsSet(i))
		}
		record.Set(enr.WithEntry("subnets", &subnetsVec))
	}

	// Sign
	err = enode.SignV4(&record, sk)
	require.NoError(t, err)

	// Create node
	node, err := enode.New(enode.V4ID{}, &record)
	require.NoError(t, err)

	return node
}

// Transform node into PeerEvent
func ToPeerEvent(node *enode.Node) PeerEvent {
	addrInfo, err := ToPeer(node)
	if err != nil {
		panic(err)
	}
	return PeerEvent{
		AddrInfo: *addrInfo,
		Node:     node,
	}
}

// Mock enode.Iterator
type MockIterator struct {
	nodes    []*enode.Node
	position int
	closed   bool
	mtx      sync.Mutex
}

func NewMockIterator(nodes []*enode.Node) *MockIterator {
	return &MockIterator{
		nodes:    nodes,
		position: -1,
	}
}

func (m *MockIterator) Next() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.closed || m.position >= len(m.nodes)-1 {
		return false
	}
	m.position++
	return m.nodes[m.position] != nil
}

func (m *MockIterator) Node() *enode.Node {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.closed || m.position == -1 || m.position >= len(m.nodes) {
		return nil
	}
	return m.nodes[m.position]
}

func (m *MockIterator) Close() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.closed = true
}

// Mock peers.ConnectionIndex
type MockConnection struct {
	connectedness map[peer.ID]network.Connectedness
	canConnect    map[peer.ID]bool
	atLimit       bool
	isBad         map[peer.ID]bool
	mu            sync.RWMutex
}

func NewMockConnection() *MockConnection {
	return &MockConnection{
		connectedness: make(map[peer.ID]network.Connectedness),
		canConnect:    make(map[peer.ID]bool),
		isBad:         make(map[peer.ID]bool),
		atLimit:       false,
	}
}

func (mc *MockConnection) Connectedness(id peer.ID) network.Connectedness {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	if conn, ok := mc.connectedness[id]; ok {
		return conn
	}
	return network.NotConnected
}

func (mc *MockConnection) CanConnect(id peer.ID) error {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	if can, ok := mc.canConnect[id]; ok && can {
		return nil
	}
	return fmt.Errorf("cannot connect")
}

func (mc *MockConnection) AtLimit(dir network.Direction) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.atLimit
}

func (mc *MockConnection) IsBad(id peer.ID) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	if bad, ok := mc.isBad[id]; ok {
		return bad
	}
	return false
}

func (mc *MockConnection) SetConnectedness(id peer.ID, conn network.Connectedness) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.connectedness[id] = conn
}

func (mc *MockConnection) SetCanConnect(id peer.ID, canConnect bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.canConnect[id] = canConnect
}

func (mc *MockConnection) SetAtLimit(atLimit bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.atLimit = atLimit
}

func (mc *MockConnection) SetIsBad(id peer.ID, isBad bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.isBad[id] = isBad
}

// Mock listener
type MockListener struct {
	localNode         *enode.LocalNode
	nodes             []*enode.Node
	closed            bool
	nodesForPingError []*enode.Node
}

func NewMockListener(localNode *enode.LocalNode, nodes []*enode.Node) *MockListener {
	return &MockListener{
		localNode:         localNode,
		nodes:             nodes,
		nodesForPingError: make([]*enode.Node, 0),
	}
}

func (l *MockListener) Lookup(enode.ID) []*enode.Node {
	return l.nodes
}
func (l *MockListener) RandomNodes() enode.Iterator {
	return NewMockIterator(l.nodes)
}
func (l *MockListener) AllNodes() []*enode.Node {
	return l.nodes
}
func (l *MockListener) Ping(node *enode.Node) error {
	nodeStr := node.String()
	for _, storedNode := range l.nodesForPingError {
		if storedNode.String() == nodeStr {
			return errors.New("failed ping")
		}
	}
	return nil
}
func (l *MockListener) LocalNode() *enode.LocalNode {
	return l.localNode
}
func (l *MockListener) Close() {
	l.closed = true
}
func (l *MockListener) SetNodesForPingError(nodes []*enode.Node) {
	l.nodesForPingError = nodes
}
