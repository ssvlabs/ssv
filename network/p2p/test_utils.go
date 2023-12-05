package p2pv1

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	p2pcommons "github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/testing"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// PeersIndexProvider holds peers index instance
type PeersIndexProvider interface {
	PeersIndex() peers.Index
}

// HostProvider holds host instance
type HostProvider interface {
	Host() host.Host
}

// LocalNet holds the nodes in the local network
type LocalNet struct {
	Nodes    []network.P2PNetwork
	NodeKeys []testing.NodeKeys
	Bootnode *discovery.Bootnode

	udpRand testing.UDPPortsRandomizer
}

// WithBootnode adds a bootnode to the network
func (ln *LocalNet) WithBootnode(ctx context.Context, logger *zap.Logger) error {
	bnSk, err := commons.GenNetworkKey()
	if err != nil {
		return err
	}
	isk, err := commons.ECDSAPrivToInterface(bnSk)
	if err != nil {
		return err
	}
	b, err := isk.Raw()
	if err != nil {
		return err
	}
	bn, err := discovery.NewBootnode(ctx, logger, &discovery.BootnodeOptions{
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

// NewTestP2pNetwork creates a new network.P2PNetwork instance
func (ln *LocalNet) NewTestP2pNetwork(ctx context.Context, nodeIndex int, keys testing.NodeKeys, logger *zap.Logger, options LocalNetOptions) (network.P2PNetwork, error) {
	operatorPubkey, err := rsaencryption.ExtractPublicKey(keys.OperatorKey)
	if err != nil {
		return nil, err
	}
	cfg := NewNetConfig(keys, format.OperatorID([]byte(operatorPubkey)), ln.Bootnode, testing.RandomTCPPort(12001, 12999), ln.udpRand.Next(13001, 13999), options.Nodes)
	cfg.Ctx = ctx
	cfg.Subnets = "00000000000000000000020000000000" //PAY ATTENTION for future test scenarios which use more than one eth-validator we need to make this field dynamically changing
	cfg.NodeStorage = mock.NodeStorage{
		MockGetPrivateKey:               keys.OperatorKey,
		RegisteredOperatorPublicKeyPEMs: []string{},
	}
	cfg.Metrics = nil
	cfg.MessageValidator = validation.NewMessageValidator(networkconfig.TestNetwork)
	cfg.Network = networkconfig.TestNetwork
	if options.TotalValidators > 0 {
		cfg.GetValidatorStats = func() (uint64, uint64, uint64, error) {
			return uint64(options.TotalValidators), uint64(options.ActiveValidators), uint64(options.MyValidators), nil
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
		cfg.MessageValidator = validation.NewMessageValidator(networkconfig.TestNetwork, validation.WithSelfAccept(selfPeerID, true))
	}

	if options.PeerScoreInspector != nil && options.PeerScoreInspectorInterval > 0 {
		cfg.PeerScoreInspector = func(peerMap map[peer.ID]*pubsub.PeerScoreSnapshot) {
			options.PeerScoreInspector(selfPeerID, peerMap)
		}
		cfg.PeerScoreInspectorInterval = options.PeerScoreInspectorInterval
	}

	p := New(logger, cfg)
	err = p.Setup(logger)
	if err != nil {
		return nil, err
	}
	return p, nil
}

type LocalNetOptions struct {
	MessageValidatorProvider                        func(int) validation.MessageValidator
	Nodes                                           int
	MinConnected                                    int
	UseDiscv5                                       bool
	TotalValidators, ActiveValidators, MyValidators int
	PeerScoreInspector                              func(selfPeer peer.ID, peerMap map[peer.ID]*pubsub.PeerScoreSnapshot)
	PeerScoreInspectorInterval                      time.Duration
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
	nodes, keys, err := testing.NewLocalTestnet(ctx, options.Nodes, func(pctx context.Context, nodeIndex int, keys testing.NodeKeys) network.P2PNetwork {
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
func NewNetConfig(keys testing.NodeKeys, operatorPubKeyHash string, bn *discovery.Bootnode, tcpPort, udpPort, maxPeers int) *Config {
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
		OperatorPrivateKey: keys.OperatorKey,
		OperatorPubKeyHash: operatorPubKeyHash,
		UserAgent:          ua,
		Discovery:          discT,
		Permissioned: func() bool {
			return false
		},
	}
}
