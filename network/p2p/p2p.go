package p2p

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"fmt"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/prysmaticlabs/prysm/async"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	p2pHost "github.com/libp2p/go-libp2p-core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
)

const (
	// DiscoveryInterval is how often we re-publish our mDNS records.
	DiscoveryInterval = time.Second

	// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
	DiscoveryServiceTag = "bloxstaking.ssv"

	// MsgChanSize is the buffer size of the message channel
	MsgChanSize = 128

	topicPrefix = "bloxstaking.ssv"
)

const (
	//baseSyncStream           = "/sync/"
	//highestDecidedStream     = baseSyncStream + "highest_decided"
	//decidedByRangeStream     = baseSyncStream + "decided_by_range"
	//lastChangeRoundMsgStream = baseSyncStream + "last_change_round"
	legacyMsgStream = "/sync/0.0.1"
)

// p2pNetwork implements network.Network interface using P2P
type p2pNetwork struct {
	ctx             context.Context
	cfg             *Config
	listenersLock   *sync.RWMutex
	dv5Listener     discv5Listener
	listeners       map[string]listener
	logger          *zap.Logger
	privKey         *ecdsa.PrivateKey
	peers           *peers.Status
	host            p2pHost.Host
	pubsub          *pubsub.PubSub
	peersIndex      PeersIndex
	operatorPrivKey *rsa.PrivateKey
	fork            forks.Fork

	psSubs       map[string]context.CancelFunc
	psTopicsLock *sync.RWMutex

	reportLastMsg bool
	nodeType      NodeType
}

// New is the constructor of p2pNetworker
func New(ctx context.Context, logger *zap.Logger, cfg *Config) (network.Network, error) {
	// init empty topics map
	cfg.Topics = make(map[string]*pubsub.Topic)

	logger = logger.With(zap.String("component", "p2p"))

	n := &p2pNetwork{
		ctx:             ctx,
		cfg:             cfg,
		listeners:       map[string]listener{},
		listenersLock:   &sync.RWMutex{},
		logger:          logger,
		operatorPrivKey: cfg.OperatorPrivateKey,
		psSubs:          make(map[string]context.CancelFunc),
		psTopicsLock:    &sync.RWMutex{},
		reportLastMsg:   cfg.ReportLastMsg,
		fork:            cfg.Fork,
		nodeType:        cfg.NodeType,
	}

	if cfg.NetworkPrivateKey != nil {
		n.privKey = cfg.NetworkPrivateKey
	} else {
		privKey, err := privKey(true)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to generate p2p private key")
		}
		n.privKey = privKey
	}
	n.cfg.BootnodesENRs = filterInvalidENRs(n.logger, TransformEnr(n.cfg.Enr))
	if len(n.cfg.BootnodesENRs) == 0 {
		n.logger.Warn("missing valid bootnode ENR")
	}

	opts, err := n.buildOptions(cfg)
	if err != nil {
		logger.Fatal("could not build libp2p options", zap.Error(err))
	}
	host, err := libp2p.New(ctx, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create p2p host")
	}
	n.host = host
	n.cfg.HostID = host.ID()
	n.logger = logger.With(zap.String("id", n.cfg.HostID.String()))
	n.logger.Info("listening on port", zap.String("addr", n.host.Addrs()[0].String()))

	var ids *identify.IDService
	// create ID service only for discv5
	if cfg.DiscoveryType == discoveryTypeDiscv5 {
		ua := n.getUserAgent()
		ids, err = identify.NewIDService(host, identify.UserAgent(ua))
		if err != nil {
			return nil, errors.Wrap(err, "Failed to create ID service")
		}
		n.logger.Info("libp2p User Agent", zap.String("value", ua))
	}
	n.peersIndex = NewPeersIndex(n.host, ids, n.logger)

	n.host.Network().Notify(n.notifee())

	ps, err := n.newGossipPubsub(cfg)
	if err != nil {
		n.logger.Error("failed to start pubsub", zap.Error(err))
		return nil, errors.Wrap(err, "failed to start pubsub")
	}
	n.pubsub = ps

	if err := n.setupDiscovery(); err != nil {
		return nil, errors.Wrap(err, "failed to setup discovery")
	}
	if err := n.startDiscovery(); err != nil {
		return nil, errors.Wrap(err, "failed to start discovery")
	}

	n.setStreamHandlers()

	n.watchPeers()

	return n, nil
}

func (n *p2pNetwork) setStreamHandlers() {
	n.setLegacyStreamHandler() // TODO - remove in v0.1.6
	//n.setHighestDecidedStreamHandler()
	//n.setDecidedByRangeStreamHandler()
	//n.setLastChangeRoundStreamHandler()
}

func (n *p2pNetwork) notifee() *libp2pnetwork.NotifyBundle {
	// TODO: add connection state
	return &libp2pnetwork.NotifyBundle{
		ConnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			go func() {
				n.trace("connected peer", zap.String("who", "networkNotifiee"),
					zap.String("conn", conn.ID()),
					zap.String("multiaddr", conn.RemoteMultiaddr().String()),
					zap.String("peerID", conn.RemotePeer().String()))
				// TODO: add connection states management
				n.peersIndex.IndexPeer(conn)
			}()
		},
		DisconnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			go func() {
				// skip if we are still connected to the peer
				if net.Connectedness(conn.RemotePeer()) == libp2pnetwork.Connected {
					return
				}
				n.trace("disconnected peer", zap.String("who", "networkNotifiee"),
					zap.String("conn", conn.ID()),
					zap.String("multiaddr", conn.RemoteMultiaddr().String()),
					zap.String("peerID", conn.RemotePeer().String()))
			}()
		},
	}
}

func (n *p2pNetwork) watchPeers() {
	async.RunEvery(n.ctx, 1*time.Minute, func() {
		// index all peers and report
		go func() {
			n.peersIndex.Run()
			reportAllConnections(n)
		}()

		// topics peers
		n.psTopicsLock.RLock()
		defer n.psTopicsLock.RUnlock()
		for name, topic := range n.cfg.Topics {
			reportTopicPeers(n, name, topic)
		}
	})
}

func (n *p2pNetwork) MaxBatch() uint64 {
	return n.cfg.MaxBatchResponse
}

// getUserAgent returns ua built upon - (nodeType, nodeVersion and operatorKey)
func (n *p2pNetwork) getUserAgent() string {
	ua := commons.GetBuildData()
	if n.operatorPrivKey != nil {
		operatorPubKey, err := rsaencryption.ExtractPublicKey(n.operatorPrivKey)
		if err != nil || len(operatorPubKey) == 0 {
			n.logger.Error("could not extract operator public key", zap.Error(err))
		}
		ua = fmt.Sprintf("%s:%s:%s", ua, n.nodeType.String(), pubKeyHash(operatorPubKey)) // TODO temp solution. need to move nodeType to enr. (also nodeVersion and pubkey?)
	}
	return fmt.Sprintf("%s:%s", ua, n.nodeType.String()) // TODO temp solution. need to move nodeType to enr. (also nodeVersion and pubkey?)
}

func (n *p2pNetwork) getOperatorPubKey() (string, error) {
	if n.operatorPrivKey != nil {
		operatorPubKey, err := rsaencryption.ExtractPublicKey(n.operatorPrivKey)
		if err != nil || len(operatorPubKey) == 0 {
			n.logger.Error("could not extract operator public key", zap.Error(err))
			return "", errors.Wrap(err, "could not extract operator public key")
		}
		return operatorPubKey, nil
	}
	return "", nil
}

func (n *p2pNetwork) trace(msg string, fields ...zap.Field) {
	if n.cfg.NetworkTrace {
		n.logger.Debug(msg, fields...)
	}
}
