package p2pv1

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
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
}

// CreateAndStartLocalNet creates a LocalNet, starts its nodes and wires them into a full mesh by dialing each
// other directly (connectMesh) — no discovery is involved, so the network forms the same way on every
// machine. The whole setup is retried (up to maxAttempts) should the mesh still not settle in time on a
// loaded machine.
func CreateAndStartLocalNet(pCtx context.Context, logger *zap.Logger, options LocalNetOptions) (*LocalNet, error) {
	attempt := func(pCtx context.Context) (*LocalNet, error) {
		ln, err := NewLocalNet(pCtx, logger, options)
		if err != nil {
			return nil, err
		}

		for i, node := range ln.Nodes {
			if err := node.Start(); err != nil {
				return ln, fmt.Errorf("could not start node %d: %w", i, err)
			}
		}
		// Bound the dial phase so a stalled dial can't burn libp2p's 60s per-edge DialPeerTimeout with
		// no shorter escape (pCtx carries no deadline); 15s matches the settle-wait budget below.
		connectCtx, cancelConnect := context.WithTimeout(pCtx, 15*time.Second)
		err = connectMesh(connectCtx, ln.Nodes)
		cancelConnect()
		if err != nil {
			return ln, err
		}

		// The dials above return connected; this only lets the connection notifications settle.
		eg, ctx := errgroup.WithContext(pCtx)
		for i, node := range ln.Nodes {
			eg.Go(func() error {
				ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()

				var peers []peer.ID
				for {
					peers = node.(HostProvider).Host().Network().Peers()
					// Break on a satisfying read before the select, so a node that already has enough
					// peers is never failed by a sibling canceling ctx first.
					if len(peers) >= options.MinConnected {
						break
					}
					select {
					case <-ctx.Done():
						return fmt.Errorf("could not find enough peers for node %d, nodes quantity = %d, found = %d: %w", i, options.Nodes, len(peers), ctx.Err())
					case <-time.After(100 * time.Millisecond):
					}
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
				// attempt returns a nil ln when NewLocalNet itself fails (e.g. CreateKeys or a node factory
				// error). Only this failure path closes nodes; a successful start returns them open.
				if ln != nil {
					for _, node := range ln.Nodes {
						if closeErr := node.Close(); closeErr != nil {
							logger.Debug("could not close a node of the failed attempt", zap.Error(closeErr))
						}
					}
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

// connectMesh dials the nodes into a full mesh by address, so the local network forms without discovery.
// Node i dials the nodes up to half a ring ahead of it, so every edge is dialed exactly once and inbound
// edges spread evenly (no node takes more than half). NewNetConfig sets DisableIPRateLimit, so the mesh is
// not bounded by the connection gater's per-IP burst or inbound-limit ceiling and forms for any node count.
func connectMesh(ctx context.Context, nodes []network.P2PNetwork) error {
	hosts := make([]host.Host, len(nodes))
	for i, node := range nodes {
		hosts[i] = node.(HostProvider).Host()
	}
	n := len(hosts)
	for i, h := range hosts {
		for k := 1; k <= n/2; k++ {
			j := (i + k) % n
			if 2*k == n && i > j {
				continue // the antipodal edge of an even ring is dialed from its lower end only
			}
			if err := h.Connect(ctx, peer.AddrInfo{ID: hosts[j].ID(), Addrs: hosts[j].Addrs()}); err != nil {
				return fmt.Errorf("node %d could not connect to node %d: %w", i, j, err)
			}
		}
	}
	return nil
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

// NewLocalNet creates the nodes of a local network; CreateAndStartLocalNet starts them and wires the mesh.
func NewLocalNet(ctx context.Context, logger *zap.Logger, options LocalNetOptions) (*LocalNet, error) {
	ln := &LocalNet{}
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
	discT := discv5Discovery
	if bn != nil {
		bns = bn.ENR
	} else {
		// No bootnode: the harness wires the mesh itself (connectMesh), so the nodes run no discovery.
		discT = noDiscovery
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
		// Every mesh edge is dialed from 127.0.0.1, so a node's inbound edges all share one IP. Leaving IP
		// rate limiting on caps inbound at ipLimitBurst (8) per IP and at inboundLimit (MaxPeers/2), which
		// breaks the mesh past ~18 nodes and couples it to MaxPeers. Disabling it also drops the pubsub
		// IP-colocation penalty that would otherwise punish every node for sharing 127.0.0.1.
		DisableIPRateLimit: true,
	}
}
