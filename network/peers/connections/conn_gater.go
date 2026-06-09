package connections

import (
	"context"
	"runtime"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	leakybucket "github.com/prysmaticlabs/prysm/v4/container/leaky-bucket"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/peers/peertrace"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/ttl"
)

const (
	// Rate, burst and period over which we allow inbound connections from a single IP.
	ipLimitRate   = 4
	ipLimitBurst  = 8
	ipLimitPeriod = 30 * time.Second

	connectionGaterPhasePeerDial = "peer_dial"
	connectionGaterPhaseAddrDial = "addr_dial"
	connectionGaterPhaseAccept   = "accept"
	connectionGaterPhaseSecured  = "secured"

	connectionGaterDecisionAllow  = "allow"
	connectionGaterDecisionReject = "reject"

	connectionGaterReasonAllowed           = "allowed"
	connectionGaterReasonDisabled          = "disabled"
	connectionGaterReasonBadPeer           = "bad_peer"
	connectionGaterReasonInboundLimit      = "inbound_limit"
	connectionGaterReasonInvalidRemoteAddr = "invalid_remote_addr"
	connectionGaterReasonIPRateLimit       = "ip_rate_limit"
	connectionGaterReasonMaxPeersLimit     = "max_peers_limit"
	connectionGaterReasonTrimmedRecently   = "trimmed_recently"
)

type IsBadPeerF func(peerID peer.ID) bool
type AtInboundLimitF func() bool

// connGater implements ConnectionGater interface:
// https://github.com/libp2p/go-libp2p/core/blob/master/connmgr/gater.go
type connGater struct {
	ctx             context.Context
	logger          *zap.Logger // struct logger to implement connmgr.ConnectionGater
	disable         bool
	atMaxPeersLimit func() bool
	ipLimiter       *leakybucket.Collector
	isBadPeer       IsBadPeerF
	atInboundLimit  AtInboundLimitF
	trimmedRecently *ttl.Map[peer.ID, struct{}]
	peerObserver    *peertrace.Observer
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(
	ctx context.Context,
	logger *zap.Logger,
	disable bool,
	atLimit func() bool,
	isBadPeer IsBadPeerF,
	atInboundLimit AtInboundLimitF,
	trimmedRecently *ttl.Map[peer.ID, struct{}],
	peerObserver *peertrace.Observer,
) connmgr.ConnectionGater {
	return &connGater{
		ctx:             ctx,
		logger:          logger.Named(log.NameConnectionGater),
		disable:         disable,
		atMaxPeersLimit: atLimit,
		ipLimiter:       leakybucket.NewCollector(ipLimitRate, ipLimitBurst, ipLimitPeriod, true),
		isBadPeer:       isBadPeer,
		atInboundLimit:  atInboundLimit,
		trimmedRecently: trimmedRecently,
		peerObserver:    peerObserver,
	}
}

// InterceptPeerDial is called on an imminent outbound peer dial request, prior
// to the addresses of that peer being available/resolved. Blocking connections
// at this stage is typical for blacklisting scenarios
func (n *connGater) InterceptPeerDial(id peer.ID) (allow bool) {
	if n.isBadPeer(id) {
		n.observeDecision(connectionGaterPhasePeerDial, connectionGaterDecisionReject, connectionGaterReasonBadPeer, id, libp2pnetwork.DirOutbound)
		n.logger.Debug("preventing outbound dial to bad peer", fields.PeerID(id))
		return false
	}
	n.observeDecision(connectionGaterPhasePeerDial, connectionGaterDecisionAllow, connectionGaterReasonAllowed, id, libp2pnetwork.DirOutbound)
	return true
}

// InterceptAddrDial is called on an imminent outbound dial to a peer on a
// particular address. Blocking connections at this stage is typical for
// address filtering.
func (n *connGater) InterceptAddrDial(id peer.ID, multiaddr ma.Multiaddr) (allow bool) {
	if n.isBadPeer(id) {
		n.observeDecision(connectionGaterPhaseAddrDial, connectionGaterDecisionReject, connectionGaterReasonBadPeer, id, libp2pnetwork.DirOutbound,
			zap.String("remote_addr", multiaddr.String()),
		)
		n.logger.Debug("preventing outbound connection due to bad peer", fields.PeerID(id))
		return false
	}
	n.observeDecision(connectionGaterPhaseAddrDial, connectionGaterDecisionAllow, connectionGaterReasonAllowed, id, libp2pnetwork.DirOutbound,
		zap.String("remote_addr", multiaddr.String()),
	)
	return true
}

// InterceptAccept is called as soon as a transport listener receives an
// inbound connection request, before any upgrade takes place. Transports who
// accept already secure and/or multiplexed connections (e.g. possibly QUIC)
// MUST call this method regardless, for correctness/consistency.
func (n *connGater) InterceptAccept(multiaddrs libp2pnetwork.ConnMultiaddrs) (allow bool) {
	if n.disable {
		n.observeDecision(connectionGaterPhaseAccept, connectionGaterDecisionAllow, connectionGaterReasonDisabled, "", libp2pnetwork.DirInbound)
		return true
	}

	remoteAddr := multiaddrs.RemoteMultiaddr()

	if n.atInboundLimit() {
		n.observeDecision(connectionGaterPhaseAccept, connectionGaterDecisionReject, connectionGaterReasonInboundLimit, "", libp2pnetwork.DirInbound)
		n.logger.Debug("connection rejected due to inbound limit",
			zap.String("remote_addr", remoteAddr.String()),
		)
		return false
	}

	allowed, reason := n.validateDial(remoteAddr)
	if !allowed {
		// Yield this goroutine to allow others to run in-between connection attempts.
		runtime.Gosched()

		n.observeDecision(connectionGaterPhaseAccept, connectionGaterDecisionReject, reason, "", libp2pnetwork.DirInbound)
		message := "connection rejected by connection gater"
		if reason == connectionGaterReasonIPRateLimit {
			message = "connection rejected due to IP rate limit"
		}
		n.logger.Debug(message,
			zap.String("reason", reason),
			zap.String("remote_addr", remoteAddr.String()),
		)
		return false
	}
	if n.atMaxPeersLimit() {
		n.observeDecision(connectionGaterPhaseAccept, connectionGaterDecisionReject, connectionGaterReasonMaxPeersLimit, "", libp2pnetwork.DirInbound)
		n.logger.Debug("connection rejected due to max peers limit",
			zap.String("remote_addr", remoteAddr.String()),
		)
		return false
	}
	n.observeDecision(connectionGaterPhaseAccept, connectionGaterDecisionAllow, connectionGaterReasonAllowed, "", libp2pnetwork.DirInbound)
	return true
}

// InterceptSecured is called for both inbound and outbound connections,
// after a security handshake has taken place and we've authenticated the peer.
func (n *connGater) InterceptSecured(direction libp2pnetwork.Direction, id peer.ID, multiaddrs libp2pnetwork.ConnMultiaddrs) (allow bool) {
	if n.trimmedRecently.Has(id) {
		n.observeDecision(connectionGaterPhaseSecured, connectionGaterDecisionReject, connectionGaterReasonTrimmedRecently, id, direction)
		n.logger.Debug(
			"InterceptSecured: trying to connect a peer we've recently trimmed",
			fields.PeerID(id),
			zap.String("conn_direction", direction.String()),
		)
		return false
	}

	if n.isBadPeer(id) {
		n.observeDecision(connectionGaterPhaseSecured, connectionGaterDecisionReject, connectionGaterReasonBadPeer, id, direction)
		n.logger.Debug("rejecting inbound connection due to bad peer", fields.PeerID(id))
		return false
	}

	n.observeDecision(connectionGaterPhaseSecured, connectionGaterDecisionAllow, connectionGaterReasonAllowed, id, direction)
	return true
}

// InterceptUpgraded is called for inbound and outbound connections, after
// libp2p has finished upgrading the connection entirely to a secure,
// multiplexed channel.
func (n *connGater) InterceptUpgraded(conn libp2pnetwork.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}

func (n *connGater) validateDial(addr ma.Multiaddr) (bool, string) {
	ip, err := manet.ToIP(addr)
	if err != nil {
		return false, connectionGaterReasonInvalidRemoteAddr
	}
	remaining := n.ipLimiter.Remaining(ip.String())
	if remaining <= 0 {
		return false, connectionGaterReasonIPRateLimit
	}
	n.ipLimiter.Add(ip.String(), 1)
	return true, connectionGaterReasonAllowed
}

func (n *connGater) observeDecision(
	phase string,
	decision string,
	reason string,
	id peer.ID,
	direction libp2pnetwork.Direction,
	extraFields ...zap.Field,
) {
	ctx := n.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	_, highlighted := n.peerObserver.Match(id)
	recordConnectionGaterDecision(ctx, phase, decision, reason, direction, highlighted)

	if id == "" {
		return
	}

	logFields := make([]zap.Field, 0, 4+len(extraFields))
	logFields = append(logFields,
		zap.String("connection_gater_phase", phase),
		zap.String("connection_gater_decision", decision),
		zap.String("connection_gater_reason", reason),
		zap.String("conn_direction", direction.String()),
	)
	logFields = append(logFields, extraFields...)
	n.peerObserver.Observe(ctx, n.loggerOrNop(), "connection_gater_decision", id, logFields...)
}

func (n *connGater) loggerOrNop() *zap.Logger {
	if n.logger == nil {
		return zap.NewNop()
	}
	return n.logger
}
