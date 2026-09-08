package p2pv1

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/ethereum/go-ethereum/common"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	p2pcommons "github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/network/testing"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/storage"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// TODO: (Alan) might have to rename this file back to test_utils.go if non-test files require it.

// LocalNet holds the nodes in the local network
type LocalNet struct {
	NodeKeys []testing.NodeKeys
	Bootnode *discovery.Bootnode
	Nodes    []network.P2PNetwork

	// mdnsTag is a per-LocalNet mDNS service tag so concurrently-running
	// test processes don't discover each other's peers.
	mdnsTag string
}

// randomMdnsTag returns a unique mDNS service tag for a test LocalNet.
func randomMdnsTag() string {
	return fmt.Sprintf("ssv.test.%016x", rand.Uint64()) //nolint: gosec // G404 is acceptable here
}

// CreateAndStartLocalNet creates a LocalNet and starts its nodes, retrying the whole setup
// (up to maxAttempts) when the mesh doesn't form in time — discovery can be slow on a loaded
// machine. Bounding the retries makes an environment where the nodes can never connect (mDNS
// discovery needs multicast) fail fast instead of hanging until the go-test timeout.
func CreateAndStartLocalNet(pCtx context.Context, logger *zap.Logger, options LocalNetOptions) (*LocalNet, error) {
	attempt := func(pCtx context.Context) (*LocalNet, error) {
		ln, err := NewLocalNet(pCtx, logger, options)
		if err != nil {
			return nil, err
		}

		eg, ctx := errgroup.WithContext(pCtx)
		// errgroup, not bare goroutines: eg.Wait() aggregates node failures so
		// a failed attempt returns an error and the loop below retries, and the
		// shared ctx cancels the siblings on the first failure so they don't
		// each block the full 15s peer-wait first.
		for i, node := range ln.Nodes {
			eg.Go(func() error {
				if err := node.Start(); err != nil {
					return fmt.Errorf("could not start node %d: %w", i, err)
				}

				ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()

				var peers []peer.ID
				for len(peers) < options.MinConnected {
					peers = node.(HostProvider).Host().Network().Peers()
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(100 * time.Millisecond):
					}
				}
				if ctx.Err() != nil {
					return fmt.Errorf("could not find enough peers for node %d, nodes quantity = %d, found = %d", i, options.Nodes, len(peers))
				}
				logger.Debug("found enough peers", zap.Int("for node", i), zap.Int("nodesQuantity", options.Nodes), zap.String("found", fmt.Sprintf("%+v", peers)))
				return nil
			})
		}

		return ln, eg.Wait()
	}

	const maxAttempts = 3
	var lastErr error
	for attemptNum := 1; ; attemptNum++ {
		select {
		case <-pCtx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("network didn't start on time: %w", lastErr)
			}
			return nil, fmt.Errorf("context is done, network didn't start on time")
		default:
			ln, err := attempt(pCtx)
			if err != nil {
				lastErr = err
				// attempt returns a nil ln when NewLocalNet itself fails
				// (e.g. CreateKeys or a node factory error), so guard before
				// ranging to avoid a nil-pointer panic that would mask err.
				if ln != nil {
					// Close the failed attempt's nodes with a timeout so a wedged
					// Close can't stall the retry loop: zeroconf's mDNS
					// Server.Shutdown can block indefinitely on a multicast socket
					// write. Only this failure path closes nodes; a successful
					// start returns them open.
					closeNodesWithTimeout(logger, ln.Nodes, 10*time.Second)
				}

				if attemptNum == maxAttempts {
					return nil, fmt.Errorf("network didn't start after %d attempts: %w", maxAttempts, lastErr)
				}
				logger.Debug("trying to relaunch local network", zap.Error(err))
				continue
			}

			return ln, nil
		}
	}
}

// closeNodesWithTimeout closes every node concurrently and waits up to timeout
// for them to finish. A node whose Close() wedges must not stall the retry loop
// in CreateAndStartLocalNet — zeroconf's mDNS Server.Shutdown can block
// indefinitely on a multicast socket write — so once the timeout elapses we stop
// waiting and let the next attempt proceed with fresh hosts and ports; an
// abandoned Close is reclaimed when the test process exits.
func closeNodesWithTimeout(logger *zap.Logger, nodes []network.P2PNetwork, timeout time.Duration) {
	var eg errgroup.Group
	for _, node := range nodes {
		eg.Go(func() error { return node.Close() })
	}
	done := make(chan struct{})
	go func() {
		_ = eg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		logger.Debug("timed out closing nodes on retry; abandoning a wedged Close (likely mDNS shutdown)",
			zap.Duration("timeout", timeout))
	}
}

type mockSignatureVerifier struct{}

func (mockSignatureVerifier) VerifySignature(operatorID spectypes.OperatorID, message *spectypes.SSVMessage, signature []byte) error {
	return nil
}

// NewTestP2pNetwork creates a new network.P2PNetwork instance
func (ln *LocalNet) NewTestP2pNetwork(ctx context.Context, nodeIndex uint64, keys testing.NodeKeys, logger *zap.Logger, options LocalNetOptions) (network.P2PNetwork, error) {
	operatorPubkey, err := keys.OperatorKey.Public().Base64()
	if err != nil {
		return nil, err
	}

	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return nil, err
	}

	nodeStorage, err := storage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
	if err != nil {
		return nil, err
	}

	for _, share := range options.Shares {
		if err := nodeStorage.Shares().Save(nil, share); err != nil {
			return nil, err
		}
	}

	for _, share := range options.Shares {
		for _, sm := range share.Committee {
			_, ok, err := nodeStorage.GetOperatorData(nil, sm.Signer)
			if err != nil {
				return nil, err
			}

			if !ok {
				_, err := nodeStorage.SaveOperatorData(nil, &registrystorage.OperatorData{
					ID:           sm.Signer,
					PublicKey:    operatorPubkey,
					OwnerAddress: common.BytesToAddress([]byte("testOwnerAddress")),
				})
				if err != nil {
					return nil, err
				}
			}
		}
	}

	dutyStore := dutystore.New()
	signatureVerifier := &mockSignatureVerifier{}

	// Use TCP/UDP port 0 so the kernel picks free ports atomically at bind time.
	cfg := NewNetConfig(keys, ln.Bootnode, 0, 0, options.Nodes)
	cfg.Ctx = ctx
	cfg.MdnsDiscoveryTag = ln.mdnsTag
	testSubnets := fixedTestSubnets(options.Shares)
	cfg.Subnets = testSubnets.StringHex()
	cfg.NodeStorage = nodeStorage
	cfg.MessageValidator = validation.New(
		networkconfig.TestNetwork,
		nodeStorage.ValidatorStore(),
		nodeStorage,
		dutyStore,
		signatureVerifier,
		// Surface verdicts (rejecting/ignoring invalid message) in test output — validation
		// defaults to a nop logger, which makes CI failures undiagnosable from logs.
		validation.WithLogger(logger),
	)
	cfg.NetworkConfig = networkconfig.TestNetwork
	if options.TotalValidators > 0 {
		cfg.GetValidatorStats = func() (uint64, uint64, uint64, error) {
			return options.TotalValidators, options.ActiveValidators, options.MyValidators, nil
		}
	}

	pubKey, err := p2pcommons.ECDSAPrivToInterface(keys.NetKey)
	if err != nil {
		panic(err)
	}
	selfPeerID, err := peer.IDFromPublicKey(pubKey.GetPublic())
	if err != nil {
		panic(err)
	}

	if options.MessageValidatorProvider != nil {
		cfg.MessageValidator = options.MessageValidatorProvider(nodeIndex)
	} else {
		cfg.MessageValidator = validation.New(
			networkconfig.TestNetwork,
			nodeStorage.ValidatorStore(),
			nodeStorage,
			dutyStore,
			signatureVerifier,
			validation.WithSelfAccept(selfPeerID, true),
			validation.WithLogger(logger),
		)
	}

	if options.PeerScoreInspector != nil && options.PeerScoreInspectorInterval > 0 {
		cfg.PeerScoreInspector = func(peerMap map[peer.ID]*pubsub.PeerScoreSnapshot) {
			options.PeerScoreInspector(selfPeerID, peerMap)
		}
		cfg.PeerScoreInspectorInterval = options.PeerScoreInspectorInterval
	}

	cfg.OperatorDataStore = operatordatastore.New(&registrystorage.OperatorData{ID: nodeIndex + 1})

	p, err := New(logger, cfg)
	if err != nil {
		return nil, err
	}
	err = p.Setup()
	if err != nil {
		return nil, err
	}
	return p, nil
}

type LocalNetOptions struct {
	MessageValidatorProvider                        func(uint64) validation.MessageValidator
	Nodes                                           int
	MinConnected                                    int
	TotalValidators, ActiveValidators, MyValidators uint64
	PeerScoreInspector                              func(selfPeer peer.ID, peerMap map[peer.ID]*pubsub.PeerScoreSnapshot)
	PeerScoreInspectorInterval                      time.Duration
	Shares                                          []*ssvtypes.SSVShare
}

// NewLocalNet creates a new mdns network
func NewLocalNet(ctx context.Context, logger *zap.Logger, options LocalNetOptions) (*LocalNet, error) {
	ln := &LocalNet{}
	ln.mdnsTag = randomMdnsTag()
	nodes, keys, err := testing.NewLocalTestnet(ctx, options.Nodes, func(pctx context.Context, nodeIndex uint64, keys testing.NodeKeys) (network.P2PNetwork, error) {
		logger := logger.Named(fmt.Sprintf("node-%d", nodeIndex))
		// The error propagates: NewLocalTestnet wraps it with the node index
		// and CreateAndStartLocalNet logs it before retrying, so there's no
		// separate error log here.
		return ln.NewTestP2pNetwork(pctx, nodeIndex, keys, logger, options)
	})
	if err != nil {
		return nil, err
	}
	ln.NodeKeys = keys
	ln.Nodes = nodes

	return ln, nil
}

// fixedTestSubnets returns the persistent subnet set used by local test networks: two fixed
// subnets (64, 90) unrelated to any share - bit positions carried over from the legacy fixture
// constant this function replaced, kept so every node also stays subscribed to subnets with no
// local committee - plus - for every configured share's committee - both
// its Alan-fork subnet (CommitteeID-hash based) and its Boole-fork subnet (lowest-operator-hash
// based). persistentSubnets is a raw, fork-agnostic bit vector (see initCfg), so covering both
// mappings here is what keeps the committee's subnet persistently subscribed on either side of
// the Boole fork, rather than depending solely on the later Subscribe(vpk) path.
func fixedTestSubnets(shares []*ssvtypes.SSVShare) p2pcommons.Subnets {
	subnets := p2pcommons.ZeroSubnets
	subnets.Set(64)
	subnets.Set(90)
	for _, share := range shares {
		subnets.Set(p2pcommons.AlanCommitteeSubnet(share.CommitteeID()))

		operators := make([]spectypes.OperatorID, 0, len(share.Committee))
		for _, member := range share.Committee {
			operators = append(operators, member.Signer)
		}
		subnets.Set(p2pcommons.BooleCommitteeSubnet(operators))
	}
	return subnets
}

// NewNetConfig creates a new config for tests
func NewNetConfig(keys testing.NodeKeys, bn *discovery.Bootnode, tcpPort, udpPort uint16, maxPeers int) *Config {
	bns := ""
	discT := "discv5"
	if bn != nil {
		bns = bn.ENR
	} else {
		discT = "mdns"
	}
	ua := ""
	return &Config{
		Bootnodes:         bns,
		TCPPort:           tcpPort,
		UDPPort:           udpPort,
		HostAddress:       "",
		HostDNS:           "",
		RequestTimeout:    10 * time.Second,
		MaxBatchResponse:  25,
		MaxPeers:          maxPeers,
		PubSubTrace:       false,
		PubSubScoring:     true,
		NetworkPrivateKey: keys.NetKey,
		UserAgent:         ua,
		Discovery:         discT,
	}
}
