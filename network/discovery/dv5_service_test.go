package discovery

import (
	"context"
	"net"
	"os"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections/mock"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/utils"
)

type TestDomainTypeProvider struct {
}

func (td *TestDomainTypeProvider) DomainType() spectypes.DomainType {
	return spectypes.DomainType{0x1, 0x2, 0x3, 0x4}
}

func (td *TestDomainTypeProvider) NextDomainType() spectypes.DomainType {
	return spectypes.DomainType{0x1, 0x2, 0x3, 0x5}
}

func (td *TestDomainTypeProvider) DomainTypeAtEpoch(epoch phase0.Epoch) spectypes.DomainType {
	return spectypes.DomainType{0x1, 0x2, 0x3, 0x4}
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
				name:           "missing domain type but has next domain type",
				domainType:     nil,
				nextDomainType: &spectypes.DomainType{0x1, 0x2, 0x3, 0x5},
				subnets:        mySubnets,
				expectedError:  errors.New("could not read domain type: not found"),
			},
			{
				name:           "domain type mismatch",
				domainType:     &spectypes.DomainType{0x1, 0x2, 0x3, 0x5},
				nextDomainType: &spectypes.DomainType{0x1, 0x2, 0x3, 0x6},
				subnets:        mySubnets,
				expectedError:  errors.New("mismatched domain type: neither 01020305 nor 01020306 match 01020304"),
			},
			{
				name:          "domain type mismatch (missing next domain type)",
				domainType:    &spectypes.DomainType{0x1, 0x2, 0x3, 0x5},
				subnets:       mySubnets,
				expectedError: errors.New("mismatched domain type: neither 01020305 nor 00000000 match 01020304"),
			},
			{
				name:           "only next domain type matches",
				domainType:     &spectypes.DomainType{0x1, 0x2, 0x3, 0x3},
				nextDomainType: &spectypes.DomainType{0x1, 0x2, 0x3, 0x4},
				subnets:        mySubnets,
				expectedError:  nil,
			},
			{
				name:           "both domain types match",
				domainType:     &spectypes.DomainType{0x1, 0x2, 0x3, 0x4},
				nextDomainType: &spectypes.DomainType{0x1, 0x2, 0x3, 0x4},
				subnets:        mySubnets,
				expectedError:  nil,
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
			if test.nextDomainType != nil {
				err := records.SetDomainTypeEntry(localNode, records.KeyNextDomainType, *test.nextDomainType)
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
	subnetIndex := peers.NewSubnetsIndex(commons.Subnets())
	dvs := &DiscV5Service{
		ctx:        ctx,
		conns:      &mock.MockConnectionIndex{LimitValue: true},
		subnetsIdx: subnetIndex,
		domainType: &TestDomainTypeProvider{},
		subnets:    mySubnets,
	}

	for _, test := range tests {
		test := test
		t.Run(test.name+":run", func(t *testing.T) {
			err := dvs.checkPeer(logger, PeerEvent{
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
	name           string
	domainType     *spectypes.DomainType
	nextDomainType *spectypes.DomainType
	subnets        []byte
	localNode      *enode.LocalNode
	expectedError  error
}

func mockSubnets(active ...int) []byte {
	subnets := make([]byte, commons.Subnets())
	for _, subnet := range active {
		subnets[subnet] = 1
	}
	return subnets
}
