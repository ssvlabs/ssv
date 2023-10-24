package connections

import (
	"net"
	"runtime"
	"time"

	"github.com/bloxapp/ssv/network/peers"
	"github.com/libp2p/go-libp2p/core/connmgr"
	libp2pcontrol "github.com/libp2p/go-libp2p/core/control"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	leakybucket "github.com/prysmaticlabs/prysm/v4/container/leaky-bucket"
	"go.uber.org/zap"
)

const (
	// Limit for rate limiter when processing new inbound dials.
	ipLimit = 4

	// Burst limit for inbound dials.
	ipBurst = 8

	// High watermark buffer signifies the buffer till which
	// we will handle inbound requests.
	highWatermarkBuffer = 10
)

type Config struct {
	AllowListCIDR string
	DenyListCIDR  []string
}

// connGater implements ConnectionGater interface: used to active inbound or outbound connection gating.
// https://github.com/libp2p/go-libp2p/core/blob/master/connmgr/gater.go
// TODO: add IP limiting
type СonnectionGater struct {
	logger     *zap.Logger // struct logger to implement connmgr.ConnectionGater
	idx        peers.ConnectionIndex
	addrFilter *multiaddr.Filters
	ipLimiter  *leakybucket.Collector
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(logger *zap.Logger, idx peers.ConnectionIndex, cfg *Config) (connmgr.ConnectionGater, error) {
	addrFilter, err := configureFilter(logger, cfg)
	if err != nil {
		logger.With(zap.String("Failed to create address filter", ""))
		return nil, err
	}
	ipLimiter := leakybucket.NewCollector(ipLimit, ipBurst, 30*time.Second, true /* deleteEmptyBuckets */)

	return &СonnectionGater{
		logger:     logger,
		idx:        idx,
		addrFilter: addrFilter,
		ipLimiter:  ipLimiter,
	}, nil
}

// InterceptPeerDial is called on an imminent outbound peer dial request, prior
// to the addresses of that peer being available/resolved. Blocking connections
// at this stage is typical for blacklisting scenarios
func (n *СonnectionGater) InterceptPeerDial(id peer.ID) bool {
	return n.idx.Limit(libp2pnetwork.DirOutbound)
}

// InterceptAddrDial tests whether we're permitted to dial the specified
// multiaddr for the given peer.
func (cg *СonnectionGater) InterceptAddrDial(pid peer.ID, m multiaddr.Multiaddr) (allow bool) {
	// Disallow bad peers from dialing in.
	if cg.idx.IsBad(cg.logger, pid) {
		return false
	}
	return filterConnections(cg.addrFilter, m)
}

// InterceptAccept checks whether the incidental inbound connection is allowed.
func (cg *СonnectionGater) InterceptAccept(n libp2pnetwork.ConnMultiaddrs) (allow bool) {
	if !cg.validateDial(n.RemoteMultiaddr()) {
		// Allow other go-routines to run in the event
		// we receive a large amount of junk connections.
		runtime.Gosched()
		return false
	}

	// TODO
	// if cg.isPeerAtLimit(true /* inbound */) {
	// 	log.WithFields(logrus.Fields{"peer": n.RemoteMultiaddr(),
	// 		"reason": "at peer limit"}).Trace("Not accepting inbound dial")
	// 	return false
	// }
	return filterConnections(cg.addrFilter, n.RemoteMultiaddr())
}

// InterceptSecured tests whether a given connection, now authenticated,
// is allowed.
func (_ *СonnectionGater) InterceptSecured(_ libp2pnetwork.Direction, _ peer.ID, _ libp2pnetwork.ConnMultiaddrs) (allow bool) {
	return true
}

// InterceptUpgraded tests whether a fully capable connection is allowed.
func (_ *СonnectionGater) InterceptUpgraded(_ libp2pnetwork.Conn) (allow bool, reason libp2pcontrol.DisconnectReason) {
	return true, 0
}

// configureFilter looks at the provided allow lists and
// deny lists to appropriately create a filter.
func configureFilter(logger *zap.Logger, cfg *Config) (*multiaddr.Filters, error) {
	addrFilter := multiaddr.NewFilters()
	var privErr error
	switch {
	case cfg.AllowListCIDR == "public":
		cfg.DenyListCIDR = append(cfg.DenyListCIDR, privateCIDRList...)
	case cfg.AllowListCIDR == "private":
		addrFilter, privErr = privateCIDRFilter(logger, addrFilter, multiaddr.ActionAccept)
		if privErr != nil {
			return nil, privErr
		}
	case cfg.AllowListCIDR != "":
		_, ipnet, err := net.ParseCIDR(cfg.AllowListCIDR)
		if err != nil {
			return nil, err
		}
		addrFilter.AddFilter(*ipnet, multiaddr.ActionAccept)
	}

	// Configure from provided deny list in the config.
	if len(cfg.DenyListCIDR) > 0 {
		for _, cidr := range cfg.DenyListCIDR {
			// If an entry in the deny list is "private", we iterate through the
			// private addresses and add them to the filter. Likewise, if the deny
			// list is "public", then we add all private address to the accept filter,
			switch {
			case cidr == "private":
				addrFilter, privErr = privateCIDRFilter(logger, addrFilter, multiaddr.ActionDeny)
				if privErr != nil {
					return nil, privErr
				}
				continue
			case cidr == "public":
				addrFilter, privErr = privateCIDRFilter(logger, addrFilter, multiaddr.ActionAccept)
				if privErr != nil {
					return nil, privErr
				}
				continue
			}
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				return nil, err
			}
			// Check if the address already has an action associated with it
			// If this address was previously accepted, log a warning before placing
			// it in the deny filter
			action, _ := addrFilter.ActionForFilter(*ipnet)
			if action == multiaddr.ActionAccept {
				logger.Warn("Address %s is in conflict with previous rule.", zap.String("", ipnet.String()))
			}
			addrFilter.AddFilter(*ipnet, multiaddr.ActionDeny)
		}
	}
	return addrFilter, nil
}

var privateCIDRList = []string{
	// Private ip addresses specified by rfc-1918.
	// See: https://tools.ietf.org/html/rfc1918
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	// Reserved address space for CGN devices, specified by rfc-6598
	// See: https://tools.ietf.org/html/rfc6598
	"100.64.0.0/10",
	// IPv4 Link-Local addresses, specified by rfc-3926
	// See: https://tools.ietf.org/html/rfc3927
	"169.254.0.0/16",
}

// helper function to either accept or deny all private addresses
// if a new rule for a private address is in conflict with a previous one, log a warning
func privateCIDRFilter(logger *zap.Logger, addrFilter *multiaddr.Filters, action multiaddr.Action) (*multiaddr.Filters, error) {
	for _, privCidr := range privateCIDRList {
		_, ipnet, err := net.ParseCIDR(privCidr)
		if err != nil {
			return nil, err
		}
		// Get the current filter action for the address
		// If it conflicts with the action given by the function call,
		// log a warning
		curAction, _ := addrFilter.ActionForFilter(*ipnet)
		switch {
		case action == multiaddr.ActionAccept:
			if curAction == multiaddr.ActionDeny {
				logger.Warn("Address is in conflict with previous rule.", zap.String("", ipnet.String()))
			}
		case action == multiaddr.ActionDeny:
			if curAction == multiaddr.ActionAccept {
				logger.Warn("Address is in conflict with previous rule.", zap.String("", ipnet.String()))
			}
		}
		addrFilter.AddFilter(*ipnet, action)
	}
	return addrFilter, nil
}

// filterConnections checks the appropriate ip subnets from our
// filter and decides what to do with them. By default libp2p
// accepts all incoming dials, so if we have an allow list
// we will reject all inbound dials except for those in the
// appropriate ip subnets.
func filterConnections(f *multiaddr.Filters, a multiaddr.Multiaddr) bool {
	acceptedNets := f.FiltersForAction(multiaddr.ActionAccept)
	restrictConns := len(acceptedNets) != 0

	// If we have an allow list added in, we by default reject all
	// connection attempts except for those coming in from the
	// appropriate ip subnets.
	if restrictConns {
		ip, err := manet.ToIP(a)
		if err != nil {
			return false
		}
		found := false
		for _, ipnet := range acceptedNets {
			if ipnet.Contains(ip) {
				found = true
				break
			}
		}
		return found
	}
	return !f.AddrBlocked(a)
}

func (cg *СonnectionGater) validateDial(addr multiaddr.Multiaddr) bool {
	ip, err := manet.ToIP(addr)
	if err != nil {
		return false
	}
	remaining := cg.ipLimiter.Remaining(ip.String())
	if remaining <= 0 {
		return false
	}
	cg.ipLimiter.Add(ip.String(), 1)
	return true
}
