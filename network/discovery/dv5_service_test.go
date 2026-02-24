package discovery

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
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
				expectedError: errors.New("domain type 01020305 doesn't match 01020304"),
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
		}
	)

	var checkPeerTestNetCfg = &networkconfig.Network{
		Beacon: networkconfig.TestNetwork.Beacon,
		SSV: &networkconfig.SSV{
			DomainType:     spectypes.DomainType{0x1, 0x2, 0x3, 0x4},
			NextDomainType: spectypes.DomainType{0x1, 0x2, 0x3, 0x4},
			Forks: networkconfig.SSVForks{
				Boole: 100,
			},
		},
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

			if test.domainType != nil {
				err := records.SetDomainTypeEntry(localNode, records.KeyDomainType, *test.domainType)
				require.NoError(t, err)
			}
			if !test.missingSubnets {
				err := records.SetSubnetsEntry(localNode, test.subnets)
				require.NoError(t, err)
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
		netCfg:              checkPeerTestNetCfg,
		subnets:             mySubnets,
		discoveredPeersPool: ttl.New[peer.ID, DiscoveredPeer](time.Hour, time.Hour),
		trimmedRecently:     ttl.New[peer.ID, struct{}](time.Hour, time.Hour),
	}

	for _, test := range tests {
		t.Run(test.name+":run", func(t *testing.T) {
			err := dvs.checkPeer(context.TODO(), PeerEvent{
				Node: test.localNode.Node(),
			})
			if test.expectedError != nil {
				require.ErrorContains(t, err, test.expectedError.Error(), test.name)
			} else {
				require.NoError(t, err, test.name)
			}
		})
	}
}

func TestIsDomainCompatibleInPostForkTransitionWindow(t *testing.T) {
	domainAlan := spectypes.DomainType{0x1, 0x2, 0x3, 0x4}
	domainBoole := spectypes.DomainType{0x5, 0x6, 0x7, 0x8}
	netCfg := &networkconfig.Network{
		Beacon: testBeaconAtEpoch(10),
		SSV: &networkconfig.SSV{
			DomainType:     domainAlan,
			NextDomainType: domainBoole,
			Forks: networkconfig.SSVForks{
				Boole: 10,
			},
		},
	}
	dvs := &DiscV5Service{netCfg: netCfg}

	slot := netCfg.EstimatedCurrentSlot()
	require.True(t, netCfg.BooleForkAtSlot(slot))
	require.True(t, netCfg.InBooleTransitionWindow(slot))
	require.False(t, dvs.isDomainCompatible(domainAlan, spectypes.DomainType{}))
	require.True(t, dvs.isDomainCompatible(domainBoole, spectypes.DomainType{}))
}

func TestIsDomainCompatibleOutsidePostForkTransitionWindow(t *testing.T) {
	domainAlan := spectypes.DomainType{0x1, 0x2, 0x3, 0x4}
	domainBoole := spectypes.DomainType{0x5, 0x6, 0x7, 0x8}
	netCfg := &networkconfig.Network{
		Beacon: testBeaconAtEpoch(11),
		SSV: &networkconfig.SSV{
			DomainType:     domainAlan,
			NextDomainType: domainBoole,
			Forks: networkconfig.SSVForks{
				Boole: 10,
			},
		},
	}
	dvs := &DiscV5Service{netCfg: netCfg}

	slot := netCfg.EstimatedCurrentSlot()
	require.True(t, netCfg.BooleForkAtSlot(slot))
	require.False(t, netCfg.InBooleTransitionWindow(slot))
	require.False(t, dvs.isDomainCompatible(domainAlan, spectypes.DomainType{}))
	require.True(t, dvs.isDomainCompatible(domainAlan, domainBoole))
	require.True(t, dvs.isDomainCompatible(domainBoole, spectypes.DomainType{}))
}

type checkPeerTest struct {
	name           string
	domainType     *spectypes.DomainType
	subnets        commons.Subnets
	missingSubnets bool
	localNode      *enode.LocalNode
	expectedError  error
}

func mockSubnets(active ...commons.Subnet) commons.Subnets {
	subnets := commons.Subnets{}
	for _, subnet := range active {
		subnets.Set(subnet)
	}
	return subnets
}

func testBeaconAtEpoch(epoch phase0.Epoch) *networkconfig.Beacon {
	beacon := *networkconfig.TestNetwork.Beacon
	slotsSinceGenesis := uint64(epoch) * beacon.SlotsPerEpoch
	beacon.GenesisTime = time.Now().Add(-time.Duration(slotsSinceGenesis) * beacon.SlotDuration)
	return &beacon
}
