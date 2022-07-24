package p2pv1

import (
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/network/streams"
	"github.com/bloxapp/ssv/network/topics"
	commons2 "github.com/bloxapp/ssv/utils/commons"
	"github.com/libp2p/go-libp2p"
	connmgr "github.com/libp2p/go-libp2p-connmgr"
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
	backoffLow = 10 * time.Second
	// backoffLow is when we stop the backoff exponent interval
	backoffHigh = 30 * time.Minute
	// backoffExponentBase is the base of the backoff exponent
	backoffExponentBase = 2.0
	// backoffConnectorCacheSize is the cache size of the backoff connector
	backoffConnectorCacheSize = 1024
	// connectTimeout is the timeout used for connections
	connectTimeout = time.Minute
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
		s := make(records.Subnets, 0)
		subnets, err := s.FromString(strings.Replace(n.cfg.Subnets, "0x", "", 1))
		if err != nil {
			// TODO: handle
			return
		}
		n.subnets = subnets
	}
	if n.cfg.MaxPeers <= 0 {
		n.cfg.MaxPeers = minPeersBuffer
	}
	if n.cfg.TopicMaxPeers <= 0 {
		n.cfg.TopicMaxPeers = minPeersBuffer / 2
	}
}

// SetupHost configures a libp2p host and backoff connector utility
func (n *p2pNetwork) SetupHost() error {
	opts, err := n.cfg.Libp2pOptions(n.fork)
	if err != nil {
		return errors.Wrap(err, "could not create libp2p options")
	}

	lowPeers, hiPeers := n.cfg.MaxPeers-3, n.cfg.MaxPeers-1
	connManager := connmgr.NewConnManager(lowPeers, hiPeers, time.Minute*5)
	opts = append(opts, libp2p.ConnectionManager(connManager))

	host, err := libp2p.New(opts...)
	if err != nil {
		return errors.Wrap(err, "could not create p2p host")
	}
	n.host = host
	n.libConnManager = connManager

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
	libPrivKey, err := commons.ConvertToInterfacePrivkey(n.cfg.NetworkPrivateKey)
	if err != nil {
		return err
	}

	self := records.NewNodeInfo(n.cfg.ForkVersion, n.cfg.NetworkID)
	self.Metadata = &records.NodeMetadata{
		OperatorID:  n.cfg.OperatorID,
		NodeVersion: commons2.GetNodeVersion(),
		Subnets:     records.Subnets(n.subnets).String(),
	}
	getPrivKey := func() crypto.PrivKey {
		return libPrivKey
	}
	n.idx = peers.NewPeersIndex(n.logger, n.host.Network(), self, n.getMaxPeers, getPrivKey, n.fork.Subnets(), 10*time.Minute)
	n.logger.Debug("peers index is ready", zap.String("forkVersion", string(n.cfg.ForkVersion)))

	ids, err := identify.NewIDService(n.host, identify.UserAgent(userAgent(n.cfg.UserAgent)))
	if err != nil {
		return errors.Wrap(err, "could not create ID service")
	}

	subnetsProvider := func() records.Subnets {
		return n.subnets
	}
	filters := []connections.HandshakeFilter{
		connections.NetworkIDFilter(n.cfg.NetworkID),
	}
	handshaker := connections.NewHandshaker(n.ctx, &connections.HandshakerCfg{
		Logger:          n.logger,
		Streams:         n.streamCtrl,
		NodeInfoIdx:     n.idx,
		States:          n.idx,
		ConnIdx:         n.idx,
		SubnetsIdx:      n.idx,
		IDService:       ids,
		Network:         n.host.Network(),
		SubnetsProvider: subnetsProvider,
	}, filters...)
	n.host.SetStreamHandler(peers.NodeInfoProtocol, handshaker.Handler())
	n.logger.Debug("handshaker is ready")

	n.connHandler = connections.NewConnHandler(n.ctx, n.logger, handshaker, subnetsProvider, n.idx, n.idx)
	n.host.Network().Notify(n.connHandler.Handle())
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
		SubnetsIdx:  n.idx,
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
	if n.cfg.PubSubTrace {
		if err := logging.SetLogLevel("pubsub", zapcore.DebugLevel.String()); err != nil {
			return errors.Wrap(err, "could not set pubsub logger level")
		}
	}
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
		OutboundQueueSize:   n.cfg.PubsubOutQueueSize,
		ValidationQueueSize: n.cfg.PubsubValidationQueueSize,
		ValidateThrottle:    n.cfg.PubsubValidateThrottle,
		MsgIDCacheTTL:       n.cfg.PubsubMsgCacheTTL,
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
