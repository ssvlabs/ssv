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
	publishENRTimeout        = time.Minute
)

// NodeProvider is an interface for managing ENRs
type NodeProvider interface {
	Self() *enode.LocalNode
	Node(info peer.AddrInfo) (*enode.Node, error)
}

// NodeFilter can be used for nodes filtering during discovery
type NodeFilter func(*enode.Node) bool

// DiscV5Service wraps discover.UDPv5 with additional functionality
// it implements go-libp2p-core/discovery.Discovery
// currently using ENR entry (subnets) to facilitate subnets discovery
// TODO: should be changed once discv5 supports topics (v5.2)
type DiscV5Service struct {
	ctx    context.Context
	logger *zap.Logger

	//connect Connect

	dv5Listener *discover.UDPv5
	bootnodes   []*enode.Node

	conns peers.ConnectionIndex
}

func newDiscV5Service(ctx context.Context, discOpts *Options) (Service, error) {
	dvs := DiscV5Service{
		ctx:    ctx,
		logger: discOpts.Logger,
		//connect: discOpts.Connect,
		conns: discOpts.ConnIndex,
	}
	dvs.logger.Debug("configuring discv5 discovery")
	if err := dvs.initDiscV5Listener(discOpts); err != nil {
		return nil, err
	}
	return &dvs, nil
}

// Self returns self node
func (dvs *DiscV5Service) Self() *enode.LocalNode {
	return dvs.dv5Listener.LocalNode()
}

// Node tries to find the enode.Node of the given peer
func (dvs *DiscV5Service) Node(info peer.AddrInfo) (*enode.Node, error) {
	pki, err := info.ID.ExtractPublicKey()
	if err != nil {
		return nil, err
	}
	pk := fromInterfacePubKey(pki)
	id := enode.PubkeyToIDV4(pk)
	logger := dvs.logger.With(zap.String("info", info.String()),
		zap.String("enode.ID", id.String()))
	nodes := dvs.dv5Listener.AllNodes()
	node := findNode(nodes, id)
	if node == nil {
		logger.Debug("could not find node, trying lookup")
		// could not find node, trying to look it up
		nodes = dvs.dv5Listener.Lookup(id)
		node = findNode(nodes, id)
	}
	logger.Debug("managed to find node")
	return node, nil
}

// Bootstrap connects to bootnodes and start looking for new nodes
// note that this function blocks
func (dvs *DiscV5Service) Bootstrap(handler HandleNewPeer) error {
	//pinged := 0
	//for _, bn := range dvs.bootnodes {
	//	err := dvs.dv5Listener.Ping(bn)
	//	if err != nil {
	//		dvs.logger.Warn("could not ping bootnode", zap.Error(err))
	//		continue
	//	}
	//	pinged++
	//}
	//if pinged == 0 {
	//	dvs.logger.Warn("could not ping bootnodes")
	//}

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
	go dvs.publishENR()

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

	ipAddr, bindIP, n := opts.IPs()

	udpConn, err := newUDPListener(bindIP, opts.Port, n)
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

// limitNodeFilter checks if limit exceeded
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

// publishENR publishes the new ENR across the network
func (dvs *DiscV5Service) publishENR() {
	ctx, done := context.WithTimeout(dvs.ctx, publishENRTimeout)
	defer done()
	dvs.discover(ctx, func(e PeerEvent) {
		err := dvs.dv5Listener.Ping(e.Node)
		if err != nil {
			dvs.logger.Warn("could not ping node", zap.String("ENR", e.Node.String()), zap.Error(err))
		}
	}, time.Millisecond*100, dvs.badNodeFilter)
}

// newUDPListener creates a udp server
func newUDPListener(bindIP net.IP, port int, network string) (*net.UDPConn, error) {
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: port,
	}
	conn, err := net.ListenUDP(network, udpAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to UDP")
	}
	return conn, nil
}
