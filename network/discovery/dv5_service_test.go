package discovery

import (
	"context"
	"math"
	"math/rand"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/jellydator/ttlcache/v3"
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
)

var TestNetwork = networkconfig.NetworkConfig{
	Beacon:            beacon.NewNetwork(spectypes.BeaconTestNetwork),
	GenesisDomainType: spectypes.DomainType{0x1, 0x2, 0x3, 0x4},
	AlanDomainType:    spectypes.DomainType{0x1, 0x2, 0x3, 0x5},
	AlanForkEpoch:     math.MaxUint64,
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
		ctx:           ctx,
		conns:         &mock.MockConnectionIndex{LimitValue: false},
		subnetsIdx:    subnetIndex,
		networkConfig: TestNetwork,
		subnets:       mySubnets,
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

// Function to generate a random peer.ID of length 32
func generateRandomPeerID() peer.ID {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	b := make([]rune, 32)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return peer.ID(b)
}

func BenchmarkTTLCacheMemoryUsage(b *testing.B) {
	rand.Seed(42) // Set seed for reproducibility

	ttl := time.Millisecond * 100
	numEntries := 10_000

	// Initialize the cache with the low TTL
	cache := ttlcache.New[peer.ID, struct{}](
		ttlcache.WithTTL[peer.ID, struct{}](ttl),
	)

	// Pre-generate all peer IDs
	peerIDs := make([]peer.ID, numEntries)
	for i := 0; i < numEntries; i++ {
		peerIDs[i] = generateRandomPeerID()
	}

	// Measure memory before insertion
	var memStatsBefore runtime.MemStats
	runtime.ReadMemStats(&memStatsBefore)

	b.Logf("before: %+v", memStatsBefore)

	// Insert entries into the cache
	for _, id := range peerIDs {
		cache.Set(id, struct{}{}, ttlcache.DefaultTTL)
	}

	// After insertion
	if err := writeMemoryProfile("mem_profile_after.prof"); err != nil {
		b.Fatalf("could not write memory profile: %v", err)
	}

	// Measure memory after insertion
	var memStatsAfter runtime.MemStats
	runtime.ReadMemStats(&memStatsAfter)
	b.Logf("after: %+v", memStatsAfter)

	b.Logf("size before delete %v", cache.Len())

	started := time.Now()
	cache.DeleteExpired()
	b.Logf("no deleting took %v", time.Since(started))

	b.Logf("size after no delete %v", cache.Len())

	// Wait for entries to expire
	time.Sleep(ttl + time.Millisecond*200)

	started = time.Now()
	cache.DeleteExpired()
	b.Logf("deleting took %v", time.Since(started))

	started = time.Now()
	l := cache.Len()
	b.Logf("size after delete %v, size took %v", l, time.Since(started))

	// Measure memory after expiration
	var memStatsFinal runtime.MemStats
	runtime.ReadMemStats(&memStatsFinal)
	b.Logf("final: %+v", memStatsFinal)
}

func warmUpGC() {
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(50 * time.Millisecond) // Give GC time to complete
	}
}

func writeMemoryProfile(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	runtime.GC() // get up-to-date statistics
	if err := pprof.WriteHeapProfile(f); err != nil {
		return err
	}
	return nil
}
