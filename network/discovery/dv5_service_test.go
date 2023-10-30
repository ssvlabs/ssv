package discovery

import (
	"context"
	"net"
	"os"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/utils"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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
				expectedError: nil,
			},
			{
				name:          "different domain type",
				domainType:    &spectypes.DomainType{0x1, 0x2, 0x3, 0x5},
				subnets:       mySubnets,
				expectedError: nil,
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
		// Create a random network key.
		priv, err := utils.ECDSAPrivateKey(logger, "")
		require.NoError(t, err)

		// Create a temporary directory for storage.
		tempDir := t.TempDir()
		defer os.RemoveAll(tempDir)

		localNode, err := records.CreateLocalNode(priv, tempDir, net.ParseIP("127.0.0.1"), 12000, 13000)
		require.NoError(t, err)

		if test.domainType != nil {
			err := records.SetDomainTypeEntry(localNode, *test.domainType)
			require.NoError(t, err)
		}
		if test.subnets != nil {
			err := records.SetSubnetsEntry(localNode, test.subnets)
			require.NoError(t, err)
		}

		test.localNode = localNode
	}

	// Run the tests.
	subnetIndex := peers.NewSubnetsIndex(commons.Subnets())
	dvs := &DiscV5Service{
		ctx:        ctx,
		conns:      &mock.MockConnectionIndex{LimitValue: true},
		subnetsIdx: subnetIndex,
		domainType: myDomainType,
		subnets:    mySubnets,
	}

	for _, test := range tests {
		err := dvs.checkPeer(logger, PeerEvent{
			Node: test.localNode.Node(),
		})
		if test.expectedError != nil {
			require.ErrorContains(t, err, test.expectedError.Error(), test.name)
		} else {
			require.NoError(t, err, test.name)
		}
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
	subnets := make([]byte, commons.Subnets())
	for _, subnet := range active {
		subnets[subnet] = 1
	}
	return subnets
}
