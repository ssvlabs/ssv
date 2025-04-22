package p2pv1

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

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
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/format"
)

// TODO: (Alan) might have to rename this file back to test_utils.go if non-test files require it.

// LocalNet holds the nodes in the local network
type LocalNet struct {
	NodeKeys []testing.NodeKeys
	Bootnode *discovery.Bootnode
	Nodes    []network.P2PNetwork

	udpRand testing.UDPPortsRandomizer
}

// WithBootnode adds a bootnode to the network
func (ln *LocalNet) WithBootnode(ctx context.Context, logger *zap.Logger) error {
	bnSk, err := p2pcommons.GenNetworkKey()
	if err != nil {
		return err
	}
	isk, err := p2pcommons.ECDSAPrivToInterface(bnSk)
	if err != nil {
		return err
	}
	b, err := isk.Raw()
	if err != nil {
		return err
	}
	bn, err := discovery.NewBootnode(ctx, logger, networkconfig.TestNetwork, &discovery.BootnodeOptions{
		PrivateKey: hex.EncodeToString(b),
		ExternalIP: "127.0.0.1",
		Port:       ln.udpRand.Next(13001, 13999),
	})
	if err != nil {
		return err
	}
	ln.Bootnode = bn
	return nil
}

// CreateAndStartLocalNet creates a new local network and starts it
// if any errors occurs during starting local network CreateAndStartLocalNet trying
// to create and start local net one more time until pCtx is not Done()
func CreateAndStartLocalNet(pCtx context.Context, logger *zap.Logger, options LocalNetOptions) (*LocalNet, error) {
	attempt := func(pCtx context.Context) (*LocalNet, error) {
		ln, err := NewLocalNet(pCtx, logger, options)
		if err != nil {
			return nil, err
		}

		eg, ctx := errgroup.WithContext(pCtx)
		for i, node := range ln.Nodes {
			i, node := i, node //hack to avoid closures. price of using error groups

			eg.Go(func() error { //if replace EG to regular goroutines round don't change to second in test
				if err := node.Start(logger); err != nil {
					return fmt.Errorf("could not start node %d: %w", i, err)
				}
				ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				var peers []peer.ID
				for len(peers) < options.MinConnected && ctx.Err() == nil {
					peers = node.(HostProvider).Host().Network().Peers()
					time.Sleep(time.Millisecond * 100)
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

	for {
		select {
		case <-pCtx.Done():
			return nil, fmt.Errorf("context is done, network didn't start on time")
		default:
			ln, err := attempt(pCtx)
			if err != nil {
				for _, node := range ln.Nodes {
					_ = node.Close()
				}

				logger.Debug("trying to relaunch local network", zap.Error(err))
				continue
			}

			return ln, nil
		}
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

	nodeStorage, err := storage.NewNodeStorage(networkconfig.TestNetwork, logger, db)
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
					PublicKey:    []byte(operatorPubkey),
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

	cfg := NewNetConfig(keys, format.OperatorID([]byte(operatorPubkey)), ln.Bootnode, testing.RandomTCPPort(12001, 12999), ln.udpRand.Next(13001, 13999), options.Nodes)
	cfg.Ctx = ctx
	cfg.Subnets = "00000000000000000100000400000400" // calculated for topics 64, 90, 114; PAY ATTENTION for future test scenarios which use more than one eth-validator we need to make this field dynamically changing
	cfg.NodeStorage = nodeStorage
	cfg.MessageValidator = validation.New(
		networkconfig.TestNetwork,
		nodeStorage.ValidatorStore(),
		nodeStorage,
		dutyStore,
		signatureVerifier,
		phase0.Epoch(0),
	)
	cfg.Network = networkconfig.TestNetwork
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
			phase0.Epoch(0),
			validation.WithSelfAccept(selfPeerID, true),
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
	err = p.Setup(logger)
	if err != nil {
		return nil, err
	}
	return p, nil
}

type LocalNetOptions struct {
	MessageValidatorProvider                        func(uint64) validation.MessageValidator
	Nodes                                           int
	MinConnected                                    int
	UseDiscv5                                       bool
	TotalValidators, ActiveValidators, MyValidators uint64
	PeerScoreInspector                              func(selfPeer peer.ID, peerMap map[peer.ID]*pubsub.PeerScoreSnapshot)
	PeerScoreInspectorInterval                      time.Duration
	Shares                                          []*ssvtypes.SSVShare
}

// NewLocalNet creates a new mdns network
func NewLocalNet(ctx context.Context, logger *zap.Logger, options LocalNetOptions) (*LocalNet, error) {
	ln := &LocalNet{}
	ln.udpRand = make(testing.UDPPortsRandomizer)
	if options.UseDiscv5 {
		if err := ln.WithBootnode(ctx, logger); err != nil {
			return nil, err
		}
	}
	nodes, keys, err := testing.NewLocalTestnet(ctx, options.Nodes, func(pctx context.Context, nodeIndex uint64, keys testing.NodeKeys) network.P2PNetwork {
		logger := logger.Named(fmt.Sprintf("node-%d", nodeIndex))
		p, err := ln.NewTestP2pNetwork(pctx, nodeIndex, keys, logger, options)
		if err != nil {
			logger.Error("could not setup network", zap.Error(err))
		}
		return p
	})
	if err != nil {
		return nil, err
	}
	ln.NodeKeys = keys
	ln.Nodes = nodes

	return ln, nil
}

// NewNetConfig creates a new config for tests
func NewNetConfig(keys testing.NodeKeys, operatorPubKeyHash string, bn *discovery.Bootnode, tcpPort, udpPort uint16, maxPeers int) *Config {
	bns := ""
	discT := "discv5"
	if bn != nil {
		bns = bn.ENR
	} else {
		discT = "mdns"
	}
	ua := ""
	return &Config{
		Bootnodes:          bns,
		TCPPort:            tcpPort,
		UDPPort:            udpPort,
		HostAddress:        "",
		HostDNS:            "",
		RequestTimeout:     10 * time.Second,
		MaxBatchResponse:   25,
		MaxPeers:           maxPeers,
		PubSubTrace:        false,
		PubSubScoring:      true,
		NetworkPrivateKey:  keys.NetKey,
		OperatorSigner:     keys.OperatorKey,
		OperatorPubKeyHash: operatorPubKeyHash,
		UserAgent:          ua,
		Discovery:          discT,
	}
}
