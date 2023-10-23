package connections

import (
	"context"
	"net"
	"sync"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"go.uber.org/zap"
)

// connGater implements ConnectionGater interface:
// https://github.com/libp2p/go-libp2p/core/blob/master/connmgr/gater.go
// TODO: add IP limiting
type connGater struct {
	sync.RWMutex
	logger *zap.Logger // struct logger to implement connmgr.ConnectionGater
	blockedPeers   map[peer.ID]struct{}
	blockedAddrs   map[string]struct{}
	blockedSubnets map[string]*net.IPNet

	ds basedb.Database
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(logger *zap.Logger, ds basedb.Database) (connmgr.ConnectionGater,error) {
	cg := &connGater{
		logger: logger,
		blockedPeers:   make(map[peer.ID]struct{}),
		blockedAddrs:   make(map[string]struct{}),
		blockedSubnets: make(map[string]*net.IPNet),
		ds: ds,
	}
	if ds != nil {
		err := cg.loadRules(context.Background())
		if err != nil {
			return nil, err
		}
	}
	return cg, nil
}

func (cg *connGater) loadRules(ctx context.Context) error {
	cg.ds.GetAll([]byte("p2p-blockedPeers"), func(i int, obj basedb.Obj) error {
		var id peer.ID
		if err := id.UnmarshalBinary(obj.Value); err != nil {
			return nil
		}
		cg.blockedPeers[id] = struct{}{}
		return nil
	})
	cg.ds.GetAll([]byte("p2p-blockedAddrs"), func(i int, obj basedb.Obj) error {
		ip := net.IP(obj.Value)
		cg.blockedAddrs[ip.String()] = struct{}{}
		return nil
	})
	cg.ds.GetAll([]byte("p2p-blockedSubnets"), func(i int, obj basedb.Obj) error {
		ipnetStr := string(obj.Value)
		_, ipnet, err := net.ParseCIDR(ipnetStr)
		if err != nil {
			cg.logger.Error("error parsing CIDR subnet: ", zap.Error(err))
			return err
		}
		cg.blockedSubnets[ipnetStr] = ipnet
		return nil
	})
	return nil
}
// InterceptPeerDial is called on an imminent outbound peer dial request, prior
// to the addresses of that peer being available/resolved. Blocking connections
// at this stage is typical for blacklisting scenarios
func (cg *connGater) InterceptPeerDial(p peer.ID) bool {
	cg.RLock()
	defer cg.RUnlock()

	_, block := cg.blockedPeers[p]
	return !block
}

// InterceptAddrDial is called on an imminent outbound dial to a peer on a
// particular address. Blocking connections at this stage is typical for
// address filtering.
func (cg *connGater) InterceptAddrDial(p peer.ID, a ma.Multiaddr) (allow bool) {
	// we have already filtered blocked peers in InterceptPeerDial, so we just check the IP
	cg.RLock()
	defer cg.RUnlock()

	ip, err := manet.ToIP(a)
	if err != nil {
		cg.logger.Warn("error converting multiaddr to IP addr:", zap.Error(err))
		return true
	}

	_, block := cg.blockedAddrs[ip.String()]
	if block {
		return false
	}

	for _, ipnet := range cg.blockedSubnets {
		if ipnet.Contains(ip) {
			return false
		}
	}

	return true
}

// InterceptAccept is called as soon as a transport listener receives an
// inbound connection request, before any upgrade takes place. Transports who
// accept already secure and/or multiplexed connections (e.g. possibly QUIC)
// MUST call this method regardless, for correctness/consistency.
func (cg *connGater) InterceptAccept(multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	cg.RLock()
	defer cg.RUnlock()

	a := multiaddrs.RemoteMultiaddr()

	ip, err := manet.ToIP(a)
	if err != nil {
		cg.logger.Warn("error converting multiaddr to IP addr:", zap.Error(err))
		return true
	}

	_, block := cg.blockedAddrs[ip.String()]
	if block {
		return false
	}

	for _, ipnet := range cg.blockedSubnets {
		if ipnet.Contains(ip) {
			return false
		}
	}

	return true
}

// InterceptSecured is called for both inbound and outbound connections,
// after a security handshake has taken place and we've authenticated the peer.
func (cg *connGater) InterceptSecured(direction libp2pnetwork.Direction, id peer.ID, multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	if direction == libp2pnetwork.DirOutbound {
		// we have already filtered those in InterceptPeerDial/InterceptAddrDial
		return true
	}

	// we have already filtered addrs in InterceptAccept, so we just check the peer ID
	cg.RLock()
	defer cg.RUnlock()

	_, block := cg.blockedPeers[id]
	return !block
}

// InterceptUpgraded is called for inbound and outbound connections, after
// libp2p has finished upgrading the connection entirely to a secure,
// multiplexed channel.
func (n *connGater) InterceptUpgraded(conn libp2pnetwork.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

// BlockPeer adds a peer to the set of blocked peers.
// Note: active connections to the peer are not automatically closed.
func (cg *connGater) BlockPeer(p peer.ID) error {
	if cg.ds != nil {
		err := cg.ds.Set([]byte("p2p-blockedPeers"), []byte(p), []byte(p))
		if err != nil {
			cg.logger.Error("error writing blocked peer to datastore: ", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()
	cg.blockedPeers[p] = struct{}{}

	return nil
}

// UnblockPeer removes a peer from the set of blocked peers
func (cg *connGater) UnblockPeer(p peer.ID) error {
	if cg.ds != nil {
		err := cg.ds.Delete([]byte("p2p-blockedPeers"), []byte(p))
		if err != nil {
			cg.logger.Error("error deleting blocked peer from datastore:", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()

	delete(cg.blockedPeers, p)

	return nil
}

// ListBlockedPeers return a list of blocked peers
func (cg *connGater) ListBlockedPeers() []peer.ID {
	cg.RLock()
	defer cg.RUnlock()

	result := make([]peer.ID, 0, len(cg.blockedPeers))
	for p := range cg.blockedPeers {
		result = append(result, p)
	}

	return result
}

// BlockAddr adds an IP address to the set of blocked addresses.
// Note: active connections to the IP address are not automatically closed.
func (cg *connGater) BlockAddr(ip net.IP) error {
	if cg.ds != nil {
		err := cg.ds.Set([]byte("p2p-blockedAddrs"), []byte(ip), []byte(ip))
		if err != nil {
			cg.logger.Error("error writing blocked addr to datastore:", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()

	cg.blockedAddrs[ip.String()] = struct{}{}

	return nil
}

// UnblockAddr removes an IP address from the set of blocked addresses
func (cg *connGater) UnblockAddr(ip net.IP) error {
	if cg.ds != nil {
		err := cg.ds.Delete([]byte("p2p-blockedAddrs"),[]byte(ip))
		if err != nil {
			cg.logger.Error("error deleting blocked addr from datastore:", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()

	delete(cg.blockedAddrs, ip.String())

	return nil
}

// ListBlockedAddrs return a list of blocked IP addresses
func (cg *connGater) ListBlockedAddrs() []net.IP {
	cg.RLock()
	defer cg.RUnlock()

	result := make([]net.IP, 0, len(cg.blockedAddrs))
	for ipStr := range cg.blockedAddrs {
		ip := net.ParseIP(ipStr)
		result = append(result, ip)
	}

	return result
}

// BlockSubnet adds an IP subnet to the set of blocked addresses.
// Note: active connections to the IP subnet are not automatically closed.
func (cg *connGater) BlockSubnet(ipnet *net.IPNet) error {
	if cg.ds != nil {
		err := cg.ds.Set([]byte("p2p-blockedSubnets"), []byte(ipnet.String()), []byte(ipnet.String()))
		if err != nil {
			cg.logger.Error("error writing blocked addr to datastore:", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()

	cg.blockedSubnets[ipnet.String()] = ipnet

	return nil
}

// UnblockSubnet removes an IP address from the set of blocked addresses
func (cg *connGater) UnblockSubnet(ipnet *net.IPNet) error {
	if cg.ds != nil {
		err := cg.ds.Delete([]byte("p2p-blockedSubnets"),  []byte(ipnet.String()))
		if err != nil {
			cg.logger.Error("error deleting blocked subnet from datastore:", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()

	delete(cg.blockedSubnets, ipnet.String())

	return nil
}

// ListBlockedSubnets return a list of blocked IP subnets
func (cg *connGater) ListBlockedSubnets() []*net.IPNet {
	cg.RLock()
	defer cg.RUnlock()

	result := make([]*net.IPNet, 0, len(cg.blockedSubnets))
	for _, ipnet := range cg.blockedSubnets {
		result = append(result, ipnet)
	}

	return result
}
