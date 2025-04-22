package discovery

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections/mock"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/utils"
	"github.com/ssvlabs/ssv/utils/ttl"
)

var TestNetwork = networkconfig.NetworkConfig{
	BeaconConfig: networkconfig.BeaconConfig{
		Beacon: beacon.NewNetwork(spectypes.BeaconTestNetwork),
	},
	SSVConfig: networkconfig.SSVConfig{
		DomainType: spectypes.DomainType{0x1, 0x2, 0x3, 0x4},
	},
}

func TestCheckPeer(t *testing.T) {
	var (
		ctx          = context.Background()
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
				name:          "missing subnets",
				domainType:    &myDomainType,
				subnets:       nil,
				expectedError: errors.New("could not read subnets"),
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

	// Create the LocalNode instances for the tests.
	for _, test := range tests {
		test := test
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
			if test.subnets != nil {
				err := records.SetSubnetsEntry(localNode, test.subnets)
				require.NoError(t, err)
			}

			test.localNode = localNode
		})
	}

	// Run the tests.
	subnetIndex := peers.NewSubnetsIndex(commons.SubnetsCount)
	dvs := &DiscV5Service{
		ctx:                 ctx,
		conns:               &mock.MockConnectionIndex{LimitValue: false},
		subnetsIdx:          subnetIndex,
		networkConfig:       TestNetwork,
		subnets:             mySubnets,
		discoveredPeersPool: ttl.New[peer.ID, DiscoveredPeer](time.Hour, time.Hour),
		trimmedRecently:     ttl.New[peer.ID, struct{}](time.Hour, time.Hour),
	}

	for _, test := range tests {
		test := test
		t.Run(test.name+":run", func(t *testing.T) {
			err := dvs.checkPeer(context.TODO(), logger, PeerEvent{
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

type checkPeerTest struct {
	name          string
	domainType    *spectypes.DomainType
	subnets       []byte
	localNode     *enode.LocalNode
	expectedError error
}

func mockSubnets(active ...int) []byte {
	subnets := make([]byte, commons.SubnetsCount)
	for _, subnet := range active {
		subnets[subnet] = 1
	}
	return subnets
}
