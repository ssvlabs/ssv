package discovery

import (
	"context"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p-core/discovery"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"net"
	"time"
)

var (
	defaultDiscoveryInterval = time.Second
)

// NodeFilter can be used for nodes filtering during discovery
type NodeFilter func(*enode.Node) bool

// DiscV5Service wraps discover.UDPv5 with additional functionality
// it implements go-libp2p-core/discovery.Discovery
// currently using ENR entry (subnets) to facilitate subnets discovery
// TODO: should be changed once discv5 supports topics (v5.2)
type DiscV5Service struct {
	ctx    context.Context
	logger *zap.Logger

	connect Connect

	dv5Listener *discover.UDPv5
	bootnodes   []*enode.Node

	conns peers.ConnectionIndex
}

func newDiscV5Service(ctx context.Context, discOpts *Options) (Service, error) {
	dvs := DiscV5Service{
		ctx:     ctx,
		logger:  discOpts.Logger,
		connect: discOpts.Connect,
		conns:   discOpts.ConnIndex,
	}
	if err := dvs.initDiscV5Listener(discOpts); err != nil {
		return nil, err
	}
	return &dvs, nil
}

// Bootstrap connects to bootnodes and start looking for new nodes
// note that this function blocks
func (dvs *DiscV5Service) Bootstrap(handler HandleNewPeer) error {
	connected := 0
	for _, bn := range dvs.bootnodes {
		pi, err := ToPeer(bn)
		if err != nil {
			dvs.logger.Warn("could not parse bootnode", zap.Error(err))
			continue
		}
		if err = dvs.connect(pi); err != nil {
			dvs.logger.Warn("could not connect to bootnode", zap.Error(err))
			continue
		}
		connected++
	}
	if connected == 0 {
		return errors.New("could not connect to bootnode")
	}

	dvs.discover(dvs.ctx, handler, defaultDiscoveryInterval,
		dvs.limitNodeFilter, dvs.badNodeFilter)

	return nil
}

// Advertise advertises a service
// implementation of discovery.Advertiser
func (dvs *DiscV5Service) Advertise(ctx context.Context, ns string, opt ...discovery.Option) (time.Duration, error) {
	opts := discovery.Options{}
	if err := opts.Apply(opt...); err != nil {
		return 0, errors.Wrap(err, "could not apply options")
	}
	if opts.Ttl == 0 {
		opts.Ttl = time.Hour
	}
	if !isSubnet(ns) {
		dvs.logger.Debug("not a subnet", zap.String("ns", ns))
		return opts.Ttl, nil
	}
	subnet := nsToSubnet(ns)
	ln := dvs.dv5Listener.LocalNode()
	subnets, err := getSubnetsEntry(ln.Node())
	if err != nil {
		return 0, errors.Wrap(err, "could not get subnets entry")
	}
	if subnets[int(subnet)] {
		dvs.logger.Debug("subnet registered", zap.String("ns", ns))
		return opts.Ttl, nil
	}
	subnets[int(subnet)] = true
	if err := setSubnetsEntry(ln, subnets); err != nil {
		return 0, errors.Wrap(err, "could not set subnets entry")
	}
	return opts.Ttl, nil
}

// FindPeers discovers peers providing a service
// implementation of discovery.Discoverer
func (dvs *DiscV5Service) FindPeers(ctx context.Context, ns string, opt ...discovery.Option) (<-chan peer.AddrInfo, error) {
	if !isSubnet(ns) {
		dvs.logger.Debug("not a subnet", zap.String("ns", ns))
		return nil, nil
	}
	cn := make(chan peer.AddrInfo, 32)
	subnet := nsToSubnet(ns)
	dvs.discover(ctx, func(e PeerEvent) {
		cn <- e.AddrInfo
	}, time.Millisecond, dvs.badNodeFilter, dvs.findBySubnetFilter(subnet))

	return cn, nil
}

// initDiscV5Listener creates a new listener and starts it
func (dvs *DiscV5Service) initDiscV5Listener(discOpts *Options) error {
	opts := discOpts.DiscV5Opts
	if err := opts.Validate(); err != nil {
		return errors.Wrap(err, "invalid opts")
	}

	ipAddr, bindIP := opts.IPs()

	udpConn, err := newUDPListener(bindIP, opts.Port)
	if err != nil {
		return errors.Wrap(err, "could not listen UDP")
	}

	localNode, err := createLocalNode(opts.NetworkKey, opts.StoragePath, ipAddr, opts.Port, opts.TCPPort)
	if err != nil {
		return errors.Wrap(err, "could not create local node")
	}

	dv5Cfg, err := opts.DiscV5Cfg()
	if err != nil {
		return err
	}
	dv5Listener, err := discover.ListenV5(udpConn, localNode, *dv5Cfg)
	if err != nil {
		return errors.Wrap(err, "could not create discV5 listener")
	}
	dvs.dv5Listener = dv5Listener
	dvs.bootnodes = dv5Cfg.Bootnodes

	return nil
}

// discover finds new nodes in the network,
// by a random walking on the underlying DHT.
//
// handler will act upon new node.
// interval enables to control the rate of new nodes that we find.
// filters will be applied on each new node before the handler is called,
// enabling to apply custom access control for different scenarios.
func (dvs *DiscV5Service) discover(ctx context.Context, handler HandleNewPeer, interval time.Duration, filters ...NodeFilter) {
	iterator := dvs.dv5Listener.RandomNodes()
	for _, f := range filters {
		iterator = enode.Filter(iterator, f)
	}
	// selfID is used to exclude current node
	selfID := dvs.dv5Listener.LocalNode().Node().ID().TerminalString()

	t := time.NewTimer(interval)
	defer t.Stop()
	wait := func() {
		t.Reset(interval)
		<-t.C
	}

	for ctx.Err() == nil {
		wait()
		exists := iterator.Next()
		if !exists {
			continue
		}
		// ignoring nil or self nodes
		if n := iterator.Node(); n != nil {
			if n.ID().TerminalString() == selfID {
				continue
			}
			ai, err := ToPeer(n)
			if err != nil {
				continue
			}
			handler(PeerEvent{
				AddrInfo: *ai,
				Node:     n,
			})
		}
	}
}

// limitNodeFilter checks if limit exceeded, then only relevant nodes will pass this filter
func (dvs *DiscV5Service) limitNodeFilter(node *enode.Node) bool {
	return !dvs.conns.Limit(libp2pnetwork.DirOutbound)
}

// badNodeFilter checks if the node was pruned or have a bad score
func (dvs *DiscV5Service) badNodeFilter(node *enode.Node) bool {
	pid, err := PeerID(node)
	if err != nil {
		dvs.logger.Warn("could not get peer ID from node record", zap.Error(err))
		return false
	}
	return !dvs.conns.IsBad(pid)
}

func (dvs *DiscV5Service) findBySubnetFilter(subnet uint64) func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		subnets, err := getSubnetsEntry(node)
		if err != nil {
			return false
		}
		return subnets[int(subnet)]
	}
}

// newUDPListener creates a udp server
func newUDPListener(bindIP net.IP, port int) (*net.UDPConn, error) {
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: port,
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to UDP")
	}
	return conn, nil
}
