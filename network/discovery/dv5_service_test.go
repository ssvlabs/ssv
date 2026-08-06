package discovery

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections/mock"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils"
	"github.com/ssvlabs/ssv/utils/ttl"
)

func TestCheckPeer(t *testing.T) {
	var (
		ctx          = t.Context()
		logger       = zap.NewNop()
		myDomainType = spectypes.DomainType{0x1, 0x2, 0x3, 0x4}
		mySubnets    = mockSubnets(1, 2, 3)
		tests        = []*checkPeerTest{
			{
				name:          "valid",
				domainType:    &myDomainType,
				subnets:       mySubnets,
				expectedError: nil,
			},
			{
				name:          "missing domain type",
				domainType:    nil,
				subnets:       mySubnets,
				expectedError: errors.New("could not read domain type: not found"),
			},
			{
				name:          "domain type mismatch",
				domainType:    &spectypes.DomainType{0x1, 0x2, 0x3, 0x5},
				subnets:       mySubnets,
				expectedError: errors.New("domain type 01020305 matches neither 01020304 nor 01020306"),
			},
			{
				name:          "matches next domain type",
				domainType:    &spectypes.DomainType{0x1, 0x2, 0x3, 0x6},
				subnets:       mySubnets,
				expectedError: nil,
			},
			{
				name:          "unrelated domain type",
				domainType:    &spectypes.DomainType{0x0, 0x0, 0x5, 0x3},
				subnets:       mySubnets,
				expectedError: errors.New("domain type 00000503 matches neither 01020304 nor 01020306"),
			},
			{
				name:           "missing subnets",
				domainType:     &myDomainType,
				subnets:        commons.Subnets{},
				missingSubnets: true,
				expectedError:  errors.New("could not read subnets"),
			},
			{
				name:          "inactive subnets",
				domainType:    &myDomainType,
				subnets:       mockSubnets(),
				expectedError: errors.New("zero subnets"),
			},
			{
				name:          "no shared subnets",
				domainType:    &myDomainType,
				subnets:       mockSubnets(0, 4, 5),
				expectedError: errors.New("no shared subnets"),
			},
			{
				name:          "one shared subnet",
				domainType:    &myDomainType,
				subnets:       mockSubnets(0, 1, 4),
				expectedError: nil,
			},
			{
				name:          "two shared subnets",
				domainType:    &myDomainType,
				subnets:       mockSubnets(0, 1, 2),
				expectedError: nil,
			},
			{
				name:              "already discovered",
				domainType:        &myDomainType,
				subnets:           mySubnets,
				alreadyDiscovered: true,
				expectedError:     errors.New("peer already discovered recently"),
			},
			{
				name:            "recently trimmed",
				domainType:      &myDomainType,
				subnets:         mySubnets,
				recentlyTrimmed: true,
				expectedError:   errors.New("peer was trimmed recently"),
			},
			{
				name:          "already connected",
				domainType:    &myDomainType,
				subnets:       mySubnets,
				connectedness: network.Connected,
				expectedError: errors.New("peer already connected"),
			},
			{
				name:          "bad peer",
				domainType:    &myDomainType,
				subnets:       mySubnets,
				isBad:         true,
				expectedError: errors.New("peer is marked bad"),
			},
			{
				name:          "not ssv",
				domainType:    &myDomainType,
				subnets:       mySubnets,
				isSSV:         boolPtr(false),
				expectedError: errors.New("node is not an SSV node"),
			},
		}
	)

	var checkPeerTestSSVConfig = &networkconfig.SSV{
		DomainType:     spectypes.DomainType{0x1, 0x2, 0x3, 0x4},
		NextDomainType: spectypes.DomainType{0x1, 0x2, 0x3, 0x6},
	}

	// Create the LocalNode instances for the tests.
	for _, test := range tests {
		t.Run(test.name+":setup", func(t *testing.T) {
			// Create a random network key.
			priv, err := utils.ECDSAPrivateKey(logger, "")
			require.NoError(t, err)

			// Create a temporary directory for storage.
			tempDir := t.TempDir()
			defer os.RemoveAll(tempDir)

			localNode, err := records.CreateLocalNode(priv, tempDir, net.ParseIP("127.0.0.1"), 12000, 13000)
			require.NoError(t, err)
			localNode.Set(enr.WithEntry("ssv", true))

			if test.domainType != nil {
				err := records.SetDomainTypeEntry(localNode, records.KeyDomainType, *test.domainType)
				require.NoError(t, err)
			}
			if !test.missingSubnets {
				err := records.SetSubnetsEntry(localNode, test.subnets)
				require.NoError(t, err)
			}
			if test.isSSV != nil {
				localNode.Set(enr.WithEntry("ssv", *test.isSSV))
			}

			test.localNode = localNode
		})
	}

	// Run the tests.
	subnetIndex := peers.NewSubnetsIndex()
	dvs := &DiscV5Service{
		ctx:                 ctx,
		conns:               &mock.MockConnectionIndex{LimitValue: false},
		subnetsIdx:          subnetIndex,
		ssvConfig:           checkPeerTestSSVConfig,
		subnets:             mySubnets,
		discoveredPeersPool: ttl.New[peer.ID, DiscoveredPeer](ctx, time.Hour, time.Hour),
		trimmedRecently:     ttl.New[peer.ID, struct{}](ctx, time.Hour, time.Hour),
	}

	for _, test := range tests {
		t.Run(test.name+":run", func(t *testing.T) {
			peerEvent := PeerEvent{
				Node: test.localNode.Node(),
			}
			addrInfo, err := ToPeer(test.localNode.Node())
			require.NoError(t, err)
			peerEvent.AddrInfo = *addrInfo

			connIndex := dvs.conns.(*mock.MockConnectionIndex)
			connIndex.ConnectednessValue = test.connectedness
			if test.isBad {
				if connIndex.BadPeers == nil {
					connIndex.BadPeers = map[peer.ID]bool{}
				}
				connIndex.BadPeers[peerEvent.AddrInfo.ID] = true
			} else if connIndex.BadPeers != nil {
				delete(connIndex.BadPeers, peerEvent.AddrInfo.ID)
			}
			if test.alreadyDiscovered {
				dvs.discoveredPeersPool.Set(peerEvent.AddrInfo.ID, DiscoveredPeer{AddrInfo: peerEvent.AddrInfo})
			} else {
				dvs.discoveredPeersPool.Delete(peerEvent.AddrInfo.ID)
			}
			if test.recentlyTrimmed {
				dvs.trimmedRecently.Set(peerEvent.AddrInfo.ID, struct{}{})
			} else {
				dvs.trimmedRecently.Delete(peerEvent.AddrInfo.ID)
			}

			err = dvs.checkPeer(context.TODO(), peerEvent)
			if test.expectedError != nil {
				require.ErrorContains(t, err, test.expectedError.Error(), test.name)
			} else {
				require.NoError(t, err, test.name)
			}
		})
	}
}

// TestCheckPeer_ZeroNextDomainType verifies the zero-value guard: when NextDomainType is
// unset (zero, as in configs built from struct literals), a peer advertising an all-zero
// domain type must still be rejected rather than matching the zero NextDomainType.
func TestCheckPeer_ZeroNextDomainType(t *testing.T) {
	ctx := t.Context()
	logger := zap.NewNop()
	mySubnets := mockSubnets(1, 2, 3)

	priv, err := utils.ECDSAPrivateKey(logger, "")
	require.NoError(t, err)

	localNode, err := records.CreateLocalNode(priv, t.TempDir(), net.ParseIP("127.0.0.1"), 12000, 13000)
	require.NoError(t, err)
	localNode.Set(enr.WithEntry("ssv", true))
	require.NoError(t, records.SetDomainTypeEntry(localNode, records.KeyDomainType, spectypes.DomainType{}))
	require.NoError(t, records.SetSubnetsEntry(localNode, mySubnets))

	addrInfo, err := ToPeer(localNode.Node())
	require.NoError(t, err)

	dvs := &DiscV5Service{
		ctx:        ctx,
		conns:      &mock.MockConnectionIndex{},
		subnetsIdx: peers.NewSubnetsIndex(),
		ssvConfig: &networkconfig.SSV{
			DomainType: spectypes.DomainType{0x1, 0x2, 0x3, 0x4},
			// NextDomainType intentionally left zero.
		},
		subnets:             mySubnets,
		discoveredPeersPool: ttl.New[peer.ID, DiscoveredPeer](ctx, time.Hour, time.Hour),
		trimmedRecently:     ttl.New[peer.ID, struct{}](ctx, time.Hour, time.Hour),
	}

	err = dvs.checkPeer(context.TODO(), PeerEvent{
		AddrInfo: *addrInfo,
		Node:     localNode.Node(),
	})
	require.ErrorContains(t, err, "domain type 00000000 does not match 01020304")
}

func TestCheckPeer_UpdatesSubnetsIndexBeforeConnectionFilters(t *testing.T) {
	ctx := t.Context()
	logger := zap.NewNop()
	myDomainType := spectypes.DomainType{0x1, 0x2, 0x3, 0x4}
	peerSubnets := mockSubnets(1, 2, 3)

	priv, err := utils.ECDSAPrivateKey(logger, "")
	require.NoError(t, err)

	tempDir := t.TempDir()
	localNode, err := records.CreateLocalNode(priv, tempDir, net.ParseIP("127.0.0.1"), 12000, 13000)
	require.NoError(t, err)
	localNode.Set(enr.WithEntry("ssv", true))
	require.NoError(t, records.SetDomainTypeEntry(localNode, records.KeyDomainType, myDomainType))
	require.NoError(t, records.SetSubnetsEntry(localNode, peerSubnets))

	addrInfo, err := ToPeer(localNode.Node())
	require.NoError(t, err)

	subnetIndex := peers.NewSubnetsIndex()
	dvs := &DiscV5Service{
		ctx:        ctx,
		conns:      &mock.MockConnectionIndex{ConnectednessValue: network.Connected},
		subnetsIdx: subnetIndex,
		ssvConfig: &networkconfig.SSV{
			DomainType: myDomainType,
		},
		subnets:             peerSubnets,
		discoveredPeersPool: ttl.New[peer.ID, DiscoveredPeer](ctx, time.Hour, time.Hour),
		trimmedRecently:     ttl.New[peer.ID, struct{}](ctx, time.Hour, time.Hour),
	}

	err = dvs.checkPeer(context.TODO(), PeerEvent{
		AddrInfo: *addrInfo,
		Node:     localNode.Node(),
	})
	require.ErrorContains(t, err, "peer already connected")

	indexedSubnets, ok := subnetIndex.GetPeerSubnets(addrInfo.ID)
	require.True(t, ok)
	require.Equal(t, peerSubnets, indexedSubnets)
}

type checkPeerTest struct {
	name              string
	domainType        *spectypes.DomainType
	subnets           commons.Subnets
	missingSubnets    bool
	localNode         *enode.LocalNode
	expectedError     error
	alreadyDiscovered bool
	recentlyTrimmed   bool
	connectedness     network.Connectedness
	isBad             bool
	isSSV             *bool
}

func mockSubnets(active ...uint64) commons.Subnets {
	subnets := commons.Subnets{}
	for _, subnet := range active {
		subnets.Set(subnet)
	}
	return subnets
}

func boolPtr(v bool) *bool {
	return &v
}
