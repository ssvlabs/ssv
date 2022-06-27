package p2pv1

import (
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/network/streams"
	"github.com/bloxapp/ssv/network/topics"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	commons2 "github.com/bloxapp/ssv/utils/commons"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	libp2pdisc "github.com/libp2p/go-libp2p-discovery"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"math/rand"
	"net"
	"strings"
	"sync/atomic"
	"time"

	logging "github.com/ipfs/go-log"
)

const (
	// defaultReqTimeout is the default timeout used for stream requests
	defaultReqTimeout = 10 * time.Second
	// backoffLow is when we start the backoff exponent interval
	backoffLow = 15 * time.Second
	// backoffLow is when we stop the backoff exponent interval
	backoffHigh = 15 * time.Minute
	// backoffExponentBase is the base of the backoff exponent
	backoffExponentBase = 2.0
	// backoffConnectorCacheSize is the cache size of the backoff connector
	backoffConnectorCacheSize = 1024
	// connectTimeout is the timeout used for connections
	connectTimeout = 15 * time.Second
	// connectorQueueSize is the buffer size of the channel used by the connector
	connectorQueueSize = 256
)

// Setup is used to setup the network
func (n *p2pNetwork) Setup() error {
	if atomic.SwapInt32(&n.state, stateInitializing) == stateReady {
		return errors.New("could not setup network: in ready state")
	}

	n.logger.Info("configuring p2p network service")

	n.initCfg()

	err := n.SetupHost()
	if err != nil {
		return err
	}
	n.logger = n.logger.With(zap.String("selfPeer", n.host.ID().String()))
	n.logger.Debug("p2p host was configured")

	err = n.SetupServices()
	if err != nil {
		return err
	}
	n.logger.Debug("p2p services were configured")

	return nil
}

func (n *p2pNetwork) initCfg() {
	if n.cfg.RequestTimeout == 0 {
		n.cfg.RequestTimeout = defaultReqTimeout
	}
	if len(n.cfg.UserAgent) == 0 {
		n.cfg.UserAgent = userAgent(n.cfg.UserAgent)
	}
	if len(n.cfg.Subnets) > 0 {
		subnets, err := parseSubnets(strings.Replace(n.cfg.Subnets, "0x", "", 1))
		if err != nil {
			// TODO: handle
			return
		}
		n.subnets = subnets
	}
}

// SetupHost configures a libp2p host and backoff connector utility
func (n *p2pNetwork) SetupHost() error {
	opts, err := n.cfg.Libp2pOptions(n.fork)
	if err != nil {
		return errors.Wrap(err, "could not create libp2p options")
	}
	host, err := libp2p.New(n.ctx, opts...)
	if err != nil {
		return errors.Wrap(err, "failed to create p2p host")
	}
	n.host = host

	backoffFactory := libp2pdisc.NewExponentialDecorrelatedJitter(backoffLow, backoffHigh, backoffExponentBase, rand.NewSource(0))
	backoffConnector, err := libp2pdisc.NewBackoffConnector(host, backoffConnectorCacheSize, connectTimeout, backoffFactory)
	if err != nil {
		return errors.Wrap(err, "could not create backoff connector")
	}
	n.backoffConnector = backoffConnector

	return nil
}

// SetupServices configures the required services
func (n *p2pNetwork) SetupServices() error {
	if err := n.setupStreamCtrl(); err != nil {
		return errors.Wrap(err, "could not setup stream controller")
	}
	if err := n.setupPeerServices(); err != nil {
		return errors.Wrap(err, "could not setup peer services")
	}
	if err := n.setupDiscovery(); err != nil {
		return errors.Wrap(err, "could not setup discovery service")
	}
	if err := n.setupPubsub(); err != nil {
		return errors.Wrap(err, "could not setup topic controller")
	}

	return nil
}

func (n *p2pNetwork) setupStreamCtrl() error {
	n.streamCtrl = streams.NewStreamController(n.ctx, n.logger, n.host, n.fork, n.cfg.RequestTimeout)
	n.logger.Debug("stream controller is ready")
	return nil
}

func (n *p2pNetwork) setupPeerServices() error {
	libPrivKey := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(n.cfg.NetworkPrivateKey))
	//self := peers.NewIdentity(n.host.ID().String(), n.cfg.OperatorID, string(n.cfg.ForkVersion), make(map[string]string))

	self := records.NewNodeInfo(n.cfg.ForkVersion, n.cfg.NetworkID)
	self.Metadata = &records.NodeMetadata{
		OperatorID:  n.cfg.OperatorID,
		NodeVersion: commons2.GetNodeVersion(),
	}
	n.idx = peers.NewPeersIndex(n.logger, n.host.Network(), self, func() int {
		return n.cfg.MaxPeers
	}, func() crypto.PrivKey {
		return libPrivKey
	}, 10*time.Minute)
	n.logger.Debug("peers index is ready", zap.String("forkVersion", string(n.cfg.ForkVersion)))

	ids, err := identify.NewIDService(n.host, identify.UserAgent(userAgent(n.cfg.UserAgent)))
	if err != nil {
		return errors.Wrap(err, "failed to create ID service")
	}

	filters := make([]peers.HandshakeFilter, 0)
	// v0 was before we checked forks, therefore asking if we are above v0
	if n.cfg.ForkVersion != forksprotocol.V0ForkVersion {
		filters = append(filters, peers.ForkVersionFilter(func() forksprotocol.ForkVersion {
			return n.cfg.ForkVersion
		}), peers.NetworkIDFilter(n.cfg.NetworkID))
	}
	handshaker := peers.NewHandshaker(n.ctx, n.logger, n.streamCtrl, n.idx, n.idx, ids, filters...)
	n.host.SetStreamHandler(peers.NodeInfoProtocol, handshaker.Handler())
	n.logger.Debug("handshaker is ready")

	connHandler := peers.HandleConnections(n.ctx, n.logger, handshaker)
	n.host.Network().Notify(connHandler)
	n.logger.Debug("connection handler is ready")
	return nil
}

func (n *p2pNetwork) setupDiscovery() error {
	ipAddr, err := commons.IPAddr()
	if err != nil {
		return errors.Wrap(err, "could not get ip addr")
	}
	var discV5Opts *discovery.DiscV5Options
	if len(n.cfg.Bootnodes) > 0 { // otherwise, we are in local scenario
		discV5Opts = &discovery.DiscV5Options{
			IP:         ipAddr.String(),
			BindIP:     net.IPv4zero.String(),
			Port:       n.cfg.UDPPort,
			TCPPort:    n.cfg.TCPPort,
			NetworkKey: n.cfg.NetworkPrivateKey,
			Bootnodes:  n.cfg.TransformBootnodes(),
			OperatorID: n.cfg.OperatorID,
		}
		if n.cfg.DiscoveryTrace {
			discV5Opts.Logger = n.logger
		}
		if len(n.subnets) > 0 {
			discV5Opts.Subnets = n.subnets
		}
		n.logger.Debug("using bootnodes to start discv5", zap.Strings("bootnodes", discV5Opts.Bootnodes))
	} else {
		n.logger.Debug("no bootnodes were configured, using mdns discovery")
	}
	discOpts := discovery.Options{
		Logger:      n.logger,
		Host:        n.host,
		DiscV5Opts:  discV5Opts,
		ConnIndex:   n.idx,
		HostAddress: n.cfg.HostAddress,
		HostDNS:     n.cfg.HostDNS,
		ForkVersion: n.cfg.ForkVersion,
	}
	disc, err := discovery.NewService(n.ctx, discOpts)
	if err != nil {
		return err
	}
	n.disc = disc

	n.logger.Debug("discovery is ready")

	return nil
}

func (n *p2pNetwork) setupPubsub() error {
	//if n.cfg.PubSubTrace {
	if err := logging.SetLogLevel("pubsub", zapcore.DebugLevel.String()); err != nil {
		return errors.Wrap(err, "could not set pubsub logger level")
	}
	//}
	cfg := &topics.PububConfig{
		Logger:   n.logger,
		Host:     n.host,
		TraceLog: n.cfg.PubSubTrace,
		MsgValidatorFactory: func(s string) topics.MsgValidatorFunc {
			logger := n.logger.With(zap.String("who", "MsgValidator"))
			return topics.NewSSVMsgValidator(logger, n.fork, n.host.ID())
		},
		MsgHandler: n.handlePubsubMessages,
		ScoreIndex: n.idx,
		//Discovery: n.disc,
	}

	if !n.cfg.PubSubScoring {
		cfg.ScoreIndex = nil
	}

	if n.fork.MsgID() != nil {
		midHandler := topics.NewMsgIDHandler(n.logger.With(zap.String("who", "msgIDHandler")),
			n.fork, time.Minute*2)
		n.msgResolver = midHandler
		cfg.MsgIDHandler = midHandler
		// run GC every 3 minutes to clear old messages
		async.RunEvery(n.ctx, time.Minute*3, midHandler.GC)
	}
	_, tc, err := topics.NewPubsub(n.ctx, cfg, n.fork)
	if err != nil {
		return errors.Wrap(err, "could not setup pubsub")
	}
	n.topicsCtrl = tc
	n.logger.Debug("topics controller is ready")
	return nil
}
