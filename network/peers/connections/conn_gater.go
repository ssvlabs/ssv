package connections

import (
	"context"
	"net"
	"sync"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	"github.com/ipfs/go-datastore/query"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/peers"
)

const (
	ns        = "/libp2p/net/conngater"
	keyPeer   = "/peer/"
	keyAddr   = "/addr/"
	keySubnet = "/subnet/"
)

// connGater implements ConnectionGater interface: used to active inbound or outbound connection gating.
// https://github.com/libp2p/go-libp2p/core/blob/master/connmgr/gater.go
// TODO: add IP limiting
type ConnectionGater struct {
	sync.RWMutex
	logger *zap.Logger // struct logger to implement connmgr.ConnectionGater
	idx    peers.ConnectionIndex

	blockedPeers   map[peer.ID]struct{}
	blockedAddrs   map[string]struct{}
	blockedSubnets map[string]*net.IPNet

	ds datastore.Datastore
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(logger *zap.Logger, idx peers.ConnectionIndex, ds datastore.Datastore) (*ConnectionGater, error) {
	cg := &ConnectionGater{
		blockedPeers:   make(map[peer.ID]struct{}),
		blockedAddrs:   make(map[string]struct{}),
		blockedSubnets: make(map[string]*net.IPNet),
	}

	if ds != nil {
		cg.ds = namespace.Wrap(ds, datastore.NewKey(ns))
		err := cg.loadRules(context.Background())
		if err != nil {
			return nil, err
		}
	}
	return &ConnectionGater{
		logger:       logger,
		idx:          idx,
		blockedPeers: make(map[peer.ID]struct{}),
		ds:           ds,
	}, nil
}

func (cg *ConnectionGater) loadRules(ctx context.Context) error {
	// load blocked peers
	res, err := cg.ds.Query(ctx, query.Query{Prefix: keyPeer})
	if err != nil {
		cg.logger.Error("error querying datastore for blocked peers: %s", zap.Error(err))
		return err
	}

	for r := range res.Next() {
		if r.Error != nil {
			cg.logger.Error("query result error: %s", zap.Error(r.Error))
			return err
		}

		p := peer.ID(r.Entry.Value)
		cg.blockedPeers[p] = struct{}{}
	}

	// load blocked addrs
	res, err = cg.ds.Query(ctx, query.Query{Prefix: keyAddr})
	if err != nil {
		cg.logger.Error("error querying datastore for blocked addrs: %s", zap.Error(err))
		return err
	}

	for r := range res.Next() {
		if r.Error != nil {
			cg.logger.Error("query result error: %s", zap.Error(r.Error))
			return err
		}

		ip := net.IP(r.Entry.Value)
		cg.blockedAddrs[ip.String()] = struct{}{}
	}

	// load blocked subnets
	res, err = cg.ds.Query(ctx, query.Query{Prefix: keySubnet})
	if err != nil {
		cg.logger.Error("error querying datastore for blocked subnets: %s", zap.Error(err))
		return err
	}

	for r := range res.Next() {
		if r.Error != nil {
			cg.logger.Error("query result error: %s", zap.Error(r.Error))
			return err
		}

		ipnetStr := string(r.Entry.Value)
		_, ipnet, err := net.ParseCIDR(ipnetStr)
		if err != nil {
			cg.logger.Error("error parsing CIDR subnet: %s", zap.Error(err))
			return err
		}
		cg.blockedSubnets[ipnetStr] = ipnet
	}

	return nil
}

// BlockPeer adds a peer to the set of blocked peers.
// Note: active connections to the peer are not automatically closed.
func (cg *ConnectionGater) BlockPeer(p peer.ID) error {
	if cg.ds != nil {
		err := cg.ds.Put(context.Background(), datastore.NewKey(keyPeer+p.String()), []byte(p))
		if err != nil {
			cg.logger.Error("error writing blocked peer to datastore: %s", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()
	cg.blockedPeers[p] = struct{}{}

	return nil
}

// UnblockPeer removes a peer from the set of blocked peers
func (cg *ConnectionGater) UnblockPeer(p peer.ID) error {
	if cg.ds != nil {
		err := cg.ds.Delete(context.Background(), datastore.NewKey(keyPeer+p.String()))
		if err != nil {
			cg.logger.Error("error deleting blocked peer from datastore: %s", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()

	delete(cg.blockedPeers, p)

	return nil
}

// ListBlockedPeers return a list of blocked peers
func (cg *ConnectionGater) ListBlockedPeers() []peer.ID {
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
func (cg *ConnectionGater) BlockAddr(ip net.IP) error {
	if cg.ds != nil {
		err := cg.ds.Put(context.Background(), datastore.NewKey(keyAddr+ip.String()), []byte(ip))
		if err != nil {
			cg.logger.Error("error writing blocked addr to datastore: %s", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()

	cg.blockedAddrs[ip.String()] = struct{}{}

	return nil
}

// UnblockAddr removes an IP address from the set of blocked addresses
func (cg *ConnectionGater) UnblockAddr(ip net.IP) error {
	if cg.ds != nil {
		err := cg.ds.Delete(context.Background(), datastore.NewKey(keyAddr+ip.String()))
		if err != nil {
			cg.logger.Error("error deleting blocked addr from datastore: %s", zap.Error(err))
			return err
		}
	}

	cg.Lock()
	defer cg.Unlock()

	delete(cg.blockedAddrs, ip.String())

	return nil
}

// ListBlockedAddrs return a list of blocked IP addresses
func (cg *ConnectionGater) ListBlockedAddrs() []net.IP {
	cg.RLock()
	defer cg.RUnlock()

	result := make([]net.IP, 0, len(cg.blockedAddrs))
	for ipStr := range cg.blockedAddrs {
		ip := net.ParseIP(ipStr)
		result = append(result, ip)
	}

	return result
}

func (cg *ConnectionGater) InterceptPeerDial(p peer.ID) (allow bool) {
	cg.RLock()
	defer cg.RUnlock()

	_, block := cg.blockedPeers[p]
	return !block
}

func (cg *ConnectionGater) InterceptAddrDial(p peer.ID, a ma.Multiaddr) (allow bool) {
	cg.RLock()
	defer cg.RUnlock()

	return true
}

func (cg *ConnectionGater) InterceptAccept(cma network.ConnMultiaddrs) (allow bool) {
	cg.RLock()
	defer cg.RUnlock()

	a := cma.RemoteMultiaddr()

	ip, err := manet.ToIP(a)
	if err != nil {
		cg.logger.Warn("error converting multiaddr to IP addr: %s", zap.Error(err))
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

func (cg *ConnectionGater) InterceptSecured(dir network.Direction, p peer.ID, cma network.ConnMultiaddrs) (allow bool) {
	if dir == network.DirOutbound {
		// we have already filtered those in InterceptPeerDial/InterceptAddrDial
		return true
	}

	// we have already filtered addrs in InterceptAccept, so we just check the peer ID
	cg.RLock()
	defer cg.RUnlock()

	_, block := cg.blockedPeers[p]
	return !block
}

func (cg *ConnectionGater) InterceptUpgraded(network.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}
