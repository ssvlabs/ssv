package connections

import (
	"context"
	"fmt"
	"net"
	"sync"

	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"go.uber.org/zap"
)

// connGater implements ConnectionGater interface: used to active inbound or outbound connection gating.
// https://github.com/libp2p/go-libp2p/core/blob/master/connmgr/gater.go
// TODO: add IP limiting
type СonnectionGater struct {
	sync.RWMutex
	logger         *zap.Logger // struct logger to implement connmgr.ConnectionGater
	blockedPeers   map[peer.ID]struct{}
	blockedAddrs   map[string]struct{}
	blockedSubnets map[string]*net.IPNet
	ds             operatorstorage.Storage
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(logger *zap.Logger, ds operatorstorage.Storage) (connmgr.ConnectionGater, error) {
	cg := &СonnectionGater{
		logger:         logger,
		blockedPeers:   make(map[peer.ID]struct{}),
		blockedAddrs:   make(map[string]struct{}),
		blockedSubnets: make(map[string]*net.IPNet),
		ds:             ds,
	}
	if ds != nil {
		err := cg.loadRules(context.Background())
		if err != nil {
			return nil, err
		}
	}
	return cg, nil
}

func (cg *СonnectionGater) loadRules(ctx context.Context) error {
	txn := cg.ds.Begin()
	defer txn.Discard()
	txn.GetAll([]byte("p2p-blockedPeers"), func(i int, obj basedb.Obj) error {
		var id peer.ID
		if err := id.UnmarshalBinary(obj.Value); err != nil {
			return nil
		}
		cg.blockedPeers[id] = struct{}{}
		return nil
	})
	txn.GetAll([]byte("p2p-blockedAddrs"), func(i int, obj basedb.Obj) error {
		ip := net.IP(obj.Value)
		cg.blockedAddrs[ip.String()] = struct{}{}
		return nil
	})
	txn.GetAll([]byte("p2p-blockedSubnets"), func(i int, obj basedb.Obj) error {
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
func (cg *СonnectionGater) InterceptPeerDial(p peer.ID) bool {
	cg.RLock()
	defer cg.RUnlock()

	_, block := cg.blockedPeers[p]
	return !block
}

// InterceptAddrDial is called on an imminent outbound dial to a peer on a
// particular address. Blocking connections at this stage is typical for
// address filtering.
func (cg *СonnectionGater) InterceptAddrDial(p peer.ID, a ma.Multiaddr) (allow bool) {
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
func (cg *СonnectionGater) InterceptAccept(multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
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
func (cg *СonnectionGater) InterceptSecured(direction libp2pnetwork.Direction, id peer.ID, multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
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
func (n *СonnectionGater) InterceptUpgraded(conn libp2pnetwork.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

// BlockPeer adds a peer to the set of blocked peers.
// Note: active connections to the peer are not automatically closed.
func (cg *СonnectionGater) BlockPeer(p peer.ID) error {
	txn := cg.ds.Begin()
	if cg.ds != nil {
		err := txn.Set([]byte("p2p-blockedPeers"), []byte(p), []byte(p))
		if err != nil {
			cg.logger.Error("error writing blocked peer to datastore: ", zap.Error(err))
			return err
		}
		if err := txn.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
	}

	cg.Lock()
	defer cg.Unlock()
	cg.blockedPeers[p] = struct{}{}

	return nil
}

// UnblockPeer removes a peer from the set of blocked peers
func (cg *СonnectionGater) UnblockPeer(p peer.ID) error {
	txn := cg.ds.Begin()
	if cg.ds != nil {
		err := txn.Delete([]byte("p2p-blockedPeers"), []byte(p))
		if err != nil {
			cg.logger.Error("error deleting blocked peer from datastore:", zap.Error(err))
			return err
		}
		if err := txn.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
	}

	cg.Lock()
	defer cg.Unlock()

	delete(cg.blockedPeers, p)

	return nil
}

// ListBlockedPeers return a list of blocked peers
func (cg *СonnectionGater) ListBlockedPeers() []peer.ID {
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
func (cg *СonnectionGater) BlockAddr(ip net.IP) error {
	txn := cg.ds.Begin()
	if cg.ds != nil {
		err := txn.Set([]byte("p2p-blockedAddrs"), []byte(ip), []byte(ip))
		if err != nil {
			cg.logger.Error("error writing blocked addr to datastore:", zap.Error(err))
			return err
		}
		if err := txn.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
	}

	cg.Lock()
	defer cg.Unlock()

	cg.blockedAddrs[ip.String()] = struct{}{}

	return nil
}

// UnblockAddr removes an IP address from the set of blocked addresses
func (cg *СonnectionGater) UnblockAddr(ip net.IP) error {
	txn := cg.ds.Begin()
	if cg.ds != nil {
		err := txn.Delete([]byte("p2p-blockedAddrs"), []byte(ip))
		if err != nil {
			cg.logger.Error("error deleting blocked addr from datastore:", zap.Error(err))
			return err
		}
		if err := txn.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
	}

	cg.Lock()
	defer cg.Unlock()

	delete(cg.blockedAddrs, ip.String())

	return nil
}

// ListBlockedAddrs return a list of blocked IP addresses
func (cg *СonnectionGater) ListBlockedAddrs() []net.IP {
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
func (cg *СonnectionGater) BlockSubnet(ipnet *net.IPNet) error {
	txn := cg.ds.Begin()
	if cg.ds != nil {
		err := txn.Set([]byte("p2p-blockedSubnets"), []byte(ipnet.String()), []byte(ipnet.String()))
		if err != nil {
			cg.logger.Error("error writing blocked addr to datastore:", zap.Error(err))
			return err
		}
		if err := txn.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
	}

	cg.Lock()
	defer cg.Unlock()

	cg.blockedSubnets[ipnet.String()] = ipnet

	return nil
}

// UnblockSubnet removes an IP address from the set of blocked addresses
func (cg *СonnectionGater) UnblockSubnet(ipnet *net.IPNet) error {
	txn := cg.ds.Begin()
	if cg.ds != nil {
		err := txn.Delete([]byte("p2p-blockedSubnets"), []byte(ipnet.String()))
		if err != nil {
			cg.logger.Error("error deleting blocked subnet from datastore:", zap.Error(err))
			return err
		}
		if err := txn.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
	}

	cg.Lock()
	defer cg.Unlock()

	delete(cg.blockedSubnets, ipnet.String())

	return nil
}

// ListBlockedSubnets return a list of blocked IP subnets
func (cg *СonnectionGater) ListBlockedSubnets() []*net.IPNet {
	cg.RLock()
	defer cg.RUnlock()

	result := make([]*net.IPNet, 0, len(cg.blockedSubnets))
	for _, ipnet := range cg.blockedSubnets {
		result = append(result, ipnet)
	}

	return result
}
