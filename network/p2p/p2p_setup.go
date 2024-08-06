package p2pv1

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	basichost "github.com/libp2p/go-libp2p/p2p/host/basic"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/async"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	p2pcommons "github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/network/streams"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/utils/commons"
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
func (n *p2pNetwork) Setup(logger *zap.Logger) error {
	logger = logger.Named(logging.NameP2PNetwork)

	if atomic.SwapInt32(&n.state, stateInitializing) == stateReady {
		return errors.New("could not setup network: in ready state")
	}

	// set a seed for rand values
	rand.Seed(time.Now().UnixNano()) // nolint: staticcheck
	logger.Info("configuring")

	if err := n.initCfg(); err != nil {
		return fmt.Errorf("init config: %w", err)
	}

	err := n.SetupHost(logger)
	if err != nil {
		return err
	}

	logger = logger.With(zap.String("selfPeer", n.host.ID().String()))
	logger.Debug("host configured")

	err = n.SetupServices(logger)
	if err != nil {
		return err
	}
	logger.Info("services configured")

	return nil
}

func (n *p2pNetwork) initCfg() error {
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
			return fmt.Errorf("parse subnet: %w", err)
		}
		n.fixedSubnets = subnets
	} else {
		n.fixedSubnets = make(records.Subnets, p2pcommons.Subnets())
	}
	if n.cfg.MaxPeers <= 0 {
		n.cfg.MaxPeers = minPeersBuffer
	}
	if n.cfg.TopicMaxPeers <= 0 {
		n.cfg.TopicMaxPeers = minPeersBuffer / 2
	}

	return nil
}

// SetupHost configures a libp2p host and backoff connector utility
func (n *p2pNetwork) SetupHost(logger *zap.Logger) error {
	opts, err := n.cfg.Libp2pOptions(logger)
	if err != nil {
		return errors.Wrap(err, "could not create libp2p options")
	}

	limitsCfg := rcmgr.DefaultLimits.AutoScale()
	// TODO: enable and extract resource manager params as config
	rmgr, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limitsCfg))
	if err != nil {
		return errors.Wrap(err, "could not create resource manager")
	}
	n.connGater = connections.NewConnectionGater(logger, n.cfg.DisableIPRateLimit, n.connectionsAtLimit)
	opts = append(opts, libp2p.ResourceManager(rmgr), libp2p.ConnectionGater(n.connGater))
	host, err := libp2p.New(opts...)
	if err != nil {
		return errors.Wrap(err, "could not create p2p host")
	}
	n.host = host
	n.libConnManager = host.ConnManager()

	backoffFactory := libp2pdiscbackoff.NewExponentialDecorrelatedJitter(backoffLow, backoffHigh, backoffExponentBase, rand.NewSource(0))
	backoffConnector, err := libp2pdiscbackoff.NewBackoffConnector(host, backoffConnectorCacheSize, connectTimeout, backoffFactory)
	if err != nil {
		return errors.Wrap(err, "could not create backoff connector")
	}
	n.backoffConnector = backoffConnector

	return nil
}

// SetupServices configures the required services
func (n *p2pNetwork) SetupServices(logger *zap.Logger) error {
	if err := n.setupStreamCtrl(logger); err != nil {
		return errors.Wrap(err, "could not setup stream controller")
	}
	if err := n.setupPeerServices(logger); err != nil {
		return errors.Wrap(err, "could not setup peer services")
	}
	if err := n.setupDiscovery(logger); err != nil {
		return errors.Wrap(err, "could not setup discovery service")
	}
	if err := n.setupPubsub(logger); err != nil {
		return errors.Wrap(err, "could not setup topic controller")
	}

	return nil
}

func (n *p2pNetwork) setupStreamCtrl(logger *zap.Logger) error {
	n.streamCtrl = streams.NewStreamController(n.ctx, n.host, n.cfg.RequestTimeout, n.cfg.RequestTimeout)
	logger.Debug("stream controller is ready")
	return nil
}

func (n *p2pNetwork) setupPeerServices(logger *zap.Logger) error {
	libPrivKey, err := p2pcommons.ECDSAPrivToInterface(n.cfg.NetworkPrivateKey)
	if err != nil {
		return err
	}
	d := n.cfg.Network.DomainType()
	domain := "0x" + hex.EncodeToString(d[:])
	self := records.NewNodeInfo(domain)
	self.Metadata = &records.NodeMetadata{
		NodeVersion: commons.GetNodeVersion(),
		Subnets:     records.Subnets(n.fixedSubnets).String(),
	}
	getPrivKey := func() crypto.PrivKey {
		return libPrivKey
	}

	n.idx = peers.NewPeersIndex(logger, n.host.Network(), self, n.getMaxPeers, getPrivKey, p2pcommons.Subnets(), 10*time.Minute)
	logger.Debug("peers index is ready")

	var ids identify.IDService
	if bh, ok := n.host.(*basichost.BasicHost); ok {
		ids = bh.IDService()
	} else {
		ids, err = identify.NewIDService(n.host, identify.UserAgent(userAgent(n.cfg.UserAgent)))
		if err != nil {
			return errors.Wrap(err, "could not create ID service")
		}
		ids.Start()
	}

	subnetsProvider := func() records.Subnets {
		return n.activeSubnets
	}

	filters := func() []connections.HandshakeFilter {
		newDomain := n.cfg.Network.DomainType()
		domain := "0x" + hex.EncodeToString(newDomain[:])
		return []connections.HandshakeFilter{
			connections.NetworkIDFilter(domain),
		}
	}

	handshaker := connections.NewHandshaker(n.ctx, &connections.HandshakerCfg{
		Streams:            n.streamCtrl,
		NodeInfos:          n.idx,
		PeerInfos:          n.idx,
		ConnIdx:            n.idx,
		SubnetsIdx:         n.idx,
		IDService:          ids,
		Network:            n.host.Network(),
		DomainTypeProvider: n.cfg.Network,
		SubnetsProvider:    subnetsProvider,
	}, filters)

	n.host.SetStreamHandler(peers.NodeInfoProtocol, handshaker.Handler(logger))
	logger.Debug("handshaker is ready")

	n.connHandler = connections.NewConnHandler(n.ctx, handshaker, subnetsProvider, n.idx, n.idx, n.idx, n.metrics)
	n.host.Network().Notify(n.connHandler.Handle(logger))
	logger.Debug("connection handler is ready")

	return nil
}

func (n *p2pNetwork) setupDiscovery(logger *zap.Logger) error {
	ipAddr, err := p2pcommons.IPAddr()
	if err != nil {
		return errors.Wrap(err, "could not get ip addr")
	}
	var discV5Opts *discovery.DiscV5Options
	if n.cfg.Discovery != localDiscvery { // otherwise, we are in local scenario
		discV5Opts = &discovery.DiscV5Options{
			IP:            ipAddr.String(),
			BindIP:        net.IPv4zero.String(),
			Port:          n.cfg.UDPPort,
			TCPPort:       n.cfg.TCPPort,
			NetworkKey:    n.cfg.NetworkPrivateKey,
			Bootnodes:     n.cfg.TransformBootnodes(),
			EnableLogging: n.cfg.DiscoveryTrace,
		}
		if len(n.fixedSubnets) > 0 {
			discV5Opts.Subnets = n.fixedSubnets
			logger = logger.With(zap.String("subnets", records.Subnets(n.fixedSubnets).String()))
		}
		logger.Info("discovery: using discv5", zap.Strings("bootnodes", discV5Opts.Bootnodes))
	} else {
		logger.Info("discovery: using mdns (local)")
	}
	discOpts := discovery.Options{
		Host:        n.host,
		DiscV5Opts:  discV5Opts,
		ConnIndex:   n.idx,
		SubnetsIdx:  n.idx,
		HostAddress: n.cfg.HostAddress,
		HostDNS:     n.cfg.HostDNS,
		DomainType:  n.cfg.Network,
	}
	disc, err := discovery.NewService(n.ctx, logger, discOpts)
	if err != nil {
		return err
	}
	n.disc = disc

	logger.Debug("discovery is ready")

	return nil
}

func (n *p2pNetwork) setupPubsub(logger *zap.Logger) error {
	cfg := &topics.PubSubConfig{
		NetworkConfig: n.cfg.Network,
		Host:          n.host,
		TraceLog:      n.cfg.PubSubTrace,
		MsgValidator:  n.msgValidator,
		MsgHandler:    n.handlePubsubMessages(logger),
		ScoreIndex:    n.idx,
		//Discovery: n.disc,
		OutboundQueueSize:   n.cfg.PubsubOutQueueSize,
		ValidationQueueSize: n.cfg.PubsubValidationQueueSize,
		ValidateThrottle:    n.cfg.PubsubValidateThrottle,
		MsgIDCacheTTL:       n.cfg.PubsubMsgCacheTTL,
		DisableIPRateLimit:  n.cfg.DisableIPRateLimit,
		GetValidatorStats:   n.cfg.GetValidatorStats,
	}

	if n.cfg.PeerScoreInspector != nil && n.cfg.PeerScoreInspectorInterval > 0 {
		cfg.ScoreInspector = n.cfg.PeerScoreInspector
		cfg.ScoreInspectorInterval = n.cfg.PeerScoreInspectorInterval
	}

	if !n.cfg.PubSubScoring {
		cfg.ScoreIndex = nil
	}

	midHandler := topics.NewMsgIDHandler(n.ctx, time.Minute*2)
	n.msgResolver = midHandler
	cfg.MsgIDHandler = midHandler
	go cfg.MsgIDHandler.Start()
	// run GC every 3 minutes to clear old messages
	async.RunEvery(n.ctx, time.Minute*3, midHandler.GC)

	_, tc, err := topics.NewPubSub(n.ctx, logger, cfg, n.metrics, n.nodeStorage.ValidatorStore())
	if err != nil {
		return errors.Wrap(err, "could not setup pubsub")
	}

	n.topicsCtrl = tc
	logger.Debug("topics controller is ready")
	return nil
}

func (n *p2pNetwork) connectionsAtLimit() bool {
	if n.idx == nil {
		return false
	}
	return n.idx.AtLimit(network.DirOutbound)
}
