package p2pv1

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	basichost "github.com/libp2p/go-libp2p/p2p/host/basic"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/prysmaticlabs/prysm/v4/async"
	"go.uber.org/zap"

	p2pcommons "github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/network/streams"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/utils/commons"
)

const (
	// defaultReqTimeout is the default timeout used for stream requests
	defaultReqTimeout = 10 * time.Second
	// backoffLow is the min value backoff strategy will use for its delay
	backoffLow = 10 * time.Second
	// backoffHigh is the max value backoff strategy will use for its delay
	backoffHigh = 5 * time.Minute
	// backoffExponentBase is the base of the backoff exponent
	backoffExponentBase = 2.0
	// backoffConnectorCacheSize is the cache size of the backoff connector
	backoffConnectorCacheSize = 2048
	// connectTimeout is the timeout used for connections
	connectTimeout = time.Minute
	// connectorQueueSize is the buffer size of the channel used by the connector
	connectorQueueSize = 2048
	// inboundLimitRatio is the ratio of inbound connections to the total connections
	// we allow (both inbound and outbound).
	inboundLimitRatio = float64(0.5)
)

// Setup is used to setup the network
func (n *p2pNetwork) Setup() error {
	logger := n.logger

	if atomic.SwapInt32(&n.state, stateInitializing) == stateReady {
		return errors.New("could not setup network: in ready state")
	}

	logger.Info("configuring")

	if err := n.initCfg(); err != nil {
		return fmt.Errorf("init config: %w", err)
	}

	if n.cfg.Libp2pTrace {
		if err := log.HookLibp2pLogging(); err != nil {
			logger.Warn("could not route go-libp2p logs through SSV logger", zap.Error(err))
		} else {
			logger.Info("routing go-libp2p logs through SSV logger (swarm2,basichost=debug)")
		}
	}

	err := n.SetupHost()
	if err != nil {
		return err
	}

	logger = logger.With(zap.String("selfPeer", n.Host().ID().String()))
	logger.Debug("host configured")

	// Let the message validator recognize the messages we publish ourselves: gossipsub validates
	// outbound messages locally, and self-discards should be logged as such rather than as publish
	// failures. Must run before SetupServices wires the validator into pubsub.
	if n.msgValidator != nil {
		n.msgValidator.SetSelfPID(n.Host().ID())
	}

	err = n.SetupServices()
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
		subnets, err := p2pcommons.SubnetsFromString(strings.Replace(n.cfg.Subnets, "0x", "", 1))
		if err != nil {
			return fmt.Errorf("parse subnet: %w", err)
		}
		n.persistentSubnets = subnets
	}
	if n.cfg.MaxPeers <= 0 {
		n.cfg.MaxPeers = minPeersBuffer
	}

	if n.cfg.TopicMaxPeers <= 0 {
		n.cfg.TopicMaxPeers = minPeersBuffer / 2
	}

	return nil
}

// IsBadPeer returns whether a peer is bad
func (n *p2pNetwork) IsBadPeer(peerID peer.ID) bool {
	if !n.isIdxSet.Load() {
		return false
	}
	return n.idx.IsBad(peerID)
}

// SetupHost configures a libp2p host and backoff connector utility
func (n *p2pNetwork) SetupHost() error {
	opts, err := n.cfg.Libp2pOptions(n.logger)
	if err != nil {
		return fmt.Errorf("could not create libp2p options: %w", err)
	}

	limitsCfg := rcmgr.DefaultLimits.AutoScale()
	// TODO: enable and extract resource manager params as config
	rmgr, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limitsCfg))
	if err != nil {
		return fmt.Errorf("could not create resource manager: %w", err)
	}
	n.connGater = connections.NewConnectionGater(
		n.logger,
		n.cfg.DisableIPRateLimit,
		n.connectionsAtLimit,
		n.IsBadPeer,
		n.atInboundLimit,
		n.trimmedRecently,
	)
	opts = append(opts, libp2p.ResourceManager(rmgr), libp2p.ConnectionGater(n.connGater))
	h, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("could not create p2p host: %w", err)
	}
	n.host.Store(&h)
	n.libConnManager = h.ConnManager()

	backoffFactory := libp2pdiscbackoff.NewExponentialDecorrelatedJitter(
		backoffLow,
		backoffHigh,
		backoffExponentBase,
		rand.NewSource(time.Now().UnixNano()),
	)
	backoffConnector, err := newBackoffConnector(n.logger, h, backoffConnectorCacheSize, connectTimeout, backoffFactory)
	if err != nil {
		return fmt.Errorf("could not create backoff connector: %w", err)
	}
	n.backoffConnector = backoffConnector

	return nil
}

// SetupServices configures the required services.
// IMPORTANT: setupPeerServices must be invoked before setupPubsub to ensure n.idx is correctly initialized.
func (n *p2pNetwork) SetupServices() error {
	if err := n.setupStreamCtrl(); err != nil {
		return fmt.Errorf("could not setup stream controller: %w", err)
	}

	if err := n.setupPeerServices(); err != nil {
		return fmt.Errorf("could not setup peer services: %w", err)
	}

	_, err := n.setupPubsub()
	if err != nil {
		return fmt.Errorf("could not setup topic controller: %w", err)
	}

	if err := n.setupDiscovery(); err != nil {
		return fmt.Errorf("could not setup discovery service: %w", err)
	}

	return nil
}

func (n *p2pNetwork) setupStreamCtrl() error {
	n.streamCtrl = streams.NewStreamController(n.ctx, n.Host(), n.cfg.RequestTimeout, n.cfg.RequestTimeout)
	n.logger.Debug("stream controller is ready")
	return nil
}

func (n *p2pNetwork) setupPeerServices() error {
	libPrivKey, err := p2pcommons.ECDSAPrivToInterface(n.cfg.NetworkPrivateKey)
	if err != nil {
		return err
	}
	d := n.cfg.NetworkConfig.CurrentDomainType()
	domain := "0x" + hex.EncodeToString(d[:])
	self := records.NewNodeInfo(domain)
	self.Metadata = &records.NodeMetadata{
		NodeVersion: commons.GetNodeVersion(),
		Subnets:     n.persistentSubnets.StringHex(),
	}
	getPrivKey := func() crypto.PrivKey {
		return libPrivKey
	}

	h := n.Host()
	n.idx = peers.NewPeersIndex(n.logger, h.Network(), self, n.getMaxPeers, getPrivKey, peers.NewGossipScoreIndex())
	n.isIdxSet.Store(true)

	n.logger.Debug("peers index is ready")

	var ids identify.IDService
	if bh, ok := h.(*basichost.BasicHost); ok {
		ids = bh.IDService()
	} else {
		ids, err = identify.NewIDService(h, identify.UserAgent(userAgent(n.cfg.UserAgent)))
		if err != nil {
			return fmt.Errorf("could not create ID service: %w", err)
		}
		ids.Start()
	}

	// Handshake filters
	filters := func() []connections.HandshakeFilter {
		currentSlot := n.cfg.NetworkConfig.EstimatedCurrentSlot()
		currentDomain := n.cfg.NetworkConfig.DomainTypeAtSlot(currentSlot)
		allowedDomains := []string{"0x" + hex.EncodeToString(currentDomain[:])}
		if n.cfg.NetworkConfig.InBooleTransitionWindow(currentSlot) {
			// During transition we accept both configured fork domains for peer compatibility.
			domainString := "0x" + hex.EncodeToString(n.cfg.NetworkConfig.DomainType[:])
			nextDomainString := "0x" + hex.EncodeToString(n.cfg.NetworkConfig.NextDomainType[:])
			if domainString == nextDomainString {
				allowedDomains = []string{domainString}
			} else {
				allowedDomains = []string{domainString, nextDomainString}
			}
		}

		networkFilter := connections.NetworkIDFilter(allowedDomains...)
		return []connections.HandshakeFilter{
			networkFilter,
			connections.BadPeerFilter(n.idx),
		}
	}

	handshaker := connections.NewHandshaker(
		n.ctx,
		n.logger,
		&connections.HandshakerCfg{
			Streams:         n.streamCtrl,
			NodeInfos:       n.idx,
			PeerInfos:       n.idx,
			ConnIdx:         n.idx,
			SubnetsIdx:      n.idx,
			IDService:       ids,
			Network:         h.Network(),
			DomainTypeFn:    n.cfg.NetworkConfig.CurrentDomainType,
			SubnetsProvider: n.ActiveSubnets,
		}, filters)

	h.SetStreamHandler(peers.NodeInfoProtocol, handshaker.Handler())
	n.logger.Debug("handshaker is ready")

	n.connHandler = connections.NewConnHandler(n.ctx, n.logger, handshaker, n.ActiveSubnets, n.idx, n.idx, n.idx, n.discoveredPeersPool)
	h.Network().Notify(n.connHandler.Handle())
	n.logger.Debug("connection handler is ready")

	return nil
}

func (n *p2pNetwork) ActiveSubnets() p2pcommons.Subnets {
	return n.currentSubnetsSnapshot()
}

func (n *p2pNetwork) FixedSubnets() p2pcommons.Subnets {
	return n.persistentSubnets
}

func (n *p2pNetwork) setupDiscovery() error {
	logger := n.logger

	var disc discovery.Service
	if n.cfg.Discovery == localDiscvery {
		logger.Info("discovery: using mdns (local)")
		var err error
		disc, err = discovery.NewLocalDiscovery(n.ctx, logger, n.Host(), n.cfg.MdnsDiscoveryTag)
		if err != nil {
			return err
		}
	} else {
		ipAddr, err := p2pcommons.IPAddr()
		if err != nil {
			return fmt.Errorf("could not get ip addr: %w", err)
		}

		discV5Opts := &discovery.DiscV5Options{
			IP:            ipAddr.String(),
			BindIP:        net.IPv4zero.String(),
			Port:          n.cfg.UDPPort,
			TCPPort:       n.cfg.TCPPort,
			NetworkKey:    n.cfg.NetworkPrivateKey,
			Bootnodes:     n.cfg.TransformBootnodes(),
			EnableLogging: n.cfg.DiscoveryTrace,
		}
		if n.persistentSubnets.HasActive() {
			discV5Opts.Subnets = n.persistentSubnets
			logger = logger.With(zap.String("persistent_subnets", n.persistentSubnets.StringHumanReadable()))
		}
		logger.Info("discovery: using discv5",
			zap.Strings("bootnodes", discV5Opts.Bootnodes),
			zap.String("ip", discV5Opts.IP),
		)

		discOpts := &discovery.Options{
			Host:                n.Host(),
			DiscV5Opts:          discV5Opts,
			ConnIndex:           n.idx,
			SubnetsIdx:          n.idx,
			HostAddress:         n.cfg.HostAddress,
			HostDNS:             n.cfg.HostDNS,
			SSVConfig:           n.cfg.NetworkConfig.SSV,
			DiscoveredPeersPool: n.discoveredPeersPool,
			TrimmedRecently:     n.trimmedRecently,
		}
		disc, err = discovery.NewDiscV5Service(n.ctx, logger, discOpts)
		if err != nil {
			return err
		}
	}
	n.disc = disc

	logger.Debug("discovery is ready")

	return nil
}

func (n *p2pNetwork) setupPubsub() (topics.Controller, error) {
	cfg := &topics.PubSubConfig{
		NetworkConfig: n.cfg.NetworkConfig,
		Host:          n.Host(),
		TraceLog:      n.cfg.PubSubTrace,
		MsgValidator:  n.msgValidator,
		MsgHandler:    n.handlePubsubMessages(),
		ScoreIndex:    n.idx,
		// Discovery: n.disc,
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

	_, tc, err := topics.NewPubSub(n.ctx, n.logger, cfg, n.nodeStorage.ValidatorStore(), n.idx)
	if err != nil {
		return nil, fmt.Errorf("could not setup pubsub: %w", err)
	}

	n.topicsCtrl = tc
	n.logger.Debug("topics controller is ready")
	return tc, nil
}

func (n *p2pNetwork) connectionsAtLimit() bool {
	if !n.isIdxSet.Load() {
		return false
	}
	return n.idx.AtLimit(network.DirOutbound)
}

func (n *p2pNetwork) atInboundLimit() bool {
	in, _ := n.connectionStats()
	inboundLimit := n.inboundLimit()
	if in >= inboundLimit {
		n.logger.Debug(
			"Preventing inbound connections due to reaching inbound limit",
			zap.Int("inbound", in),
			zap.Int("inbound_limit", inboundLimit),
			zap.Int("max_peers", n.cfg.MaxPeers),
		)
		return true
	}

	return false
}

func (n *p2pNetwork) inboundLimit() int {
	return int(float64(n.cfg.MaxPeers) * inboundLimitRatio)
}

// connectionStats returns the number of inbound and outbound connections.
//
// The Host() nil check matters here: the connection gater can fire
// InterceptAccept from libp2p listener goroutines while SetupHost is still
// inside libp2p.New(), i.e. before the host has been stored. Host() reads
// the pointer atomically, so the check is race-free (see #2448). Pre-setup
// there are no connections, so (0, 0) is the correct answer.
func (n *p2pNetwork) connectionStats() (inbound, outbound int) {
	h := n.Host()
	if h == nil {
		return 0, 0
	}
	return connectionStats(h)
}

func connectionStats(host host.Host) (inbound, outbound int) {
	for _, cn := range host.Network().Conns() {
		dir := cn.Stat().Direction
		if dir == network.DirUnknown {
			continue
		}
		if dir == network.DirOutbound {
			outbound++
		} else {
			inbound++
		}
	}
	return inbound, outbound
}
