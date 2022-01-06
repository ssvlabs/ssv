package p2p

import (
	"context"
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers/scorers"
	"go.uber.org/zap"
	"net"
	"time"
)

// discv5Listener represents the discv5 interface
type discv5Listener interface {
	Self() *enode.Node
	Close()
	Lookup(enode.ID) []*enode.Node
	Resolve(*enode.Node) *enode.Node
	RandomNodes() enode.Iterator
	Ping(*enode.Node) error
	RequestENR(*enode.Node) (*enode.Node, error)
	LocalNode() *enode.LocalNode
}

// setupDiscV5 creates all the required objects for discv5
func (n *p2pNetwork) setupDiscV5() (*discover.UDPv5, error) {
	n.peers = peers.NewStatus(n.ctx, &peers.StatusConfig{
		PeerLimit: n.maxPeers * 2, // using a larger buffer to enable discovery of many nodes as possible
		ScorerParams: &scorers.Config{
			BadResponsesScorerConfig: &scorers.BadResponsesScorerConfig{
				Threshold:     5,
				DecayInterval: time.Hour,
			},
		},
	})
	ip, err := ipAddr()
	if err != nil {
		return nil, err
	}
	listener, err := n.createListener(ip)
	if err != nil {
		return nil, errors.Wrap(err, "could not create listener")
	}
	record := listener.Self()
	n.logger.Info("Self ENR", zap.String("enr", record.String()))
	return listener, nil
}

// createListener creates a new discv5 listener
func (n *p2pNetwork) createListener(ipAddr net.IP) (*discover.UDPv5, error) {
	var bindIP net.IP
	switch udpVersionFromIP(ipAddr) {
	case udp4:
		bindIP = net.IPv4zero
	case udp6:
		bindIP = net.IPv6zero
	default:
		return nil, errors.New("invalid ip provided")
	}

	//// If Local ip is specified then use that instead.
	//if s.cfg.LocalIP != "" {
	//	ipAddr = net.ParseIP(s.cfg.LocalIP)
	//	if ipAddr == nil {
	//		return nil, errors.New("invalid Local ip provided")
	//	}
	//	bindIP = ipAddr
	//}
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: n.cfg.UDPPort,
	}
	// Listen to all network interfaces
	// for both ip protocols.
	networkVersion := "udp"
	conn, err := net.ListenUDP(networkVersion, udpAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to UDP")
	}

	localNode, err := n.createExtendedLocalNode(ipAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not create Local node")
	}

	dv5Cfg := discover.Config{
		PrivateKey: n.privKey,
	}
	if n.cfg.NetworkTrace {
		logger := log.New()
		logger.SetHandler(&dv5Logger{n.logger.With(zap.String("who", "dv5Logger"))})
		dv5Cfg.Log = logger
	}
	dv5Cfg.Bootnodes, err = parseENRs(n.cfg.BootnodesENRs, true)
	if err != nil {
		return nil, errors.Wrap(err, "could not read bootstrap addresses")
	}
	// create discv5 listener
	listener, err := discover.ListenV5(conn, localNode, dv5Cfg)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to discV5")
	}
	return listener, nil
}

// createExtendedLocalNode creates an extended enode.LocalNode with all the needed entries to be part of its enr
func (n *p2pNetwork) createExtendedLocalNode(ipAddr net.IP) (*enode.LocalNode, error) {
	operatorPubKey, err := n.getOperatorPubKey()
	if err != nil {
		return nil, err
	}
	localNode, err := createLocalNode(
		n.privKey,
		ipAddr,
		n.cfg.UDPPort,
		n.cfg.TCPPort,
	)
	if err != nil {
		return nil, errors.Wrap(err, "could not create Local node")
	}

	if len(operatorPubKey) > 0 {
		localNode, err = addOperatorIDEntry(localNode, operatorID(operatorPubKey))
		if err != nil {
			return nil, errors.Wrap(err, "could not create operator id entry")
		}
	}

	localNode, err = addNodeTypeEntry(localNode, n.nodeType)
	if err != nil {
		return nil, errors.Wrap(err, "could not create node type entry")
	}

	// TODO: add fork entry once applicable
	//localNode, err = addForkEntry(localNode, s.genesisTime, s.genesisValidatorsRoot)
	//if err != nil {
	//	return nil, errors.Wrap(err, "could not add eth2 fork version entry to enr")
	//}

	// update local node to use provided host address
	if n.cfg.HostAddress != "" {
		hostIP := net.ParseIP(n.cfg.HostAddress)
		if hostIP.To4() == nil && hostIP.To16() == nil {
			n.logger.Error("Invalid host address given", zap.String("hostIp", hostIP.String()))
		} else {
			n.logger.Info("using external IP", zap.String("IP from config", n.cfg.HostAddress), zap.String("IP", hostIP.String()))
			localNode.SetFallbackIP(hostIP)
			localNode.SetStaticIP(hostIP)
		}
	}
	// update local node to use provided host DNS
	if n.cfg.HostDNS != "" {
		_host := n.cfg.HostDNS
		ips, err := net.LookupIP(_host)
		if err != nil {
			return nil, errors.Wrap(err, "could not resolve host address")
		}
		if len(ips) > 0 {
			// Use first IP returned from the
			// resolver.
			firstIP := ips[0]
			n.logger.Info("using DNS IP", zap.String("DNS", n.cfg.HostDNS), zap.String("IP", firstIP.String()))
			localNode.SetFallbackIP(firstIP)
		}
	}
	return localNode, nil
}

// createLocalNode create a new enode.LocalNode instance
func createLocalNode(privKey *ecdsa.PrivateKey, ipAddr net.IP, udpPort, tcpPort int) (*enode.LocalNode, error) {
	db, err := enode.OpenDB("")
	if err != nil {
		return nil, errors.Wrap(err, "could not open node's peer database")
	}
	localNode := enode.NewLocalNode(db, privKey)

	ipEntry := enr.IP(ipAddr)
	udpEntry := enr.UDP(udpPort)
	tcpEntry := enr.TCP(tcpPort)
	localNode.Set(ipEntry)
	localNode.Set(udpEntry)
	localNode.Set(tcpEntry)
	localNode.SetFallbackIP(ipAddr)
	localNode.SetFallbackUDP(udpPort)

	return localNode, nil
}

// listenForNewNodes watches for new nodes in the network and connects to unknown peers.
func (n *p2pNetwork) listenForNewNodes(ctx context.Context) {
	defer n.logger.Debug("done listening for new nodes")
	iterator := n.dv5Listener.RandomNodes()
	//iterator = enode.Filter(iterator, s.filterPeer)
	defer iterator.Close()
	nextNode := func() *enode.Node {
		exists := iterator.Next()
		if !exists {
			return nil
		}
		return iterator.Node()
	}
	n.logger.Debug("starting to listen for new nodes")
	for {
		if ctx.Err() != nil {
			break
		}
		if n.isPeerAtLimit() {
			if node := nextNode(); node != nil {
				go n.tryNode(node)
			}
			n.logger.Debug("at peer limit")
			time.Sleep(6 * time.Second)
			continue
		}
		node := nextNode()
		if node == nil {
			time.Sleep(1 * time.Second)
			continue
		}
		go func(node *enode.Node) {
			if info, err := n.connectNode(node); info == nil {
				n.trace("invalid node", zap.String("enr", node.String()), zap.Error(err))
			} else if err != nil {
				n.trace("can't connect node", zap.String("enr", node.String()), zap.String("peerID", info.ID.String()), zap.Error(err))
			} else {
				n.trace("discovered node is now connected", zap.String("enr", node.String()), zap.String("peer", info.ID.String()))
			}
		}(node)
	}
}

// isPeerAtLimit checks for max peers
func (n *p2pNetwork) isPeerAtLimit() bool {
	numOfConns := len(n.host.Network().Peers())
	activePeers := len(n.peers.Active())
	return activePeers >= n.maxPeers || numOfConns >= n.maxPeers
}

// isPeerAtLimit checks for max peers
func (n *p2pNetwork) shouldConnectByUserAgent(id peer.ID) bool {
	if n.lookupHandler == nil {
		return false
	}
	if n.host.ID().String() == id.String() {
		n.trace("peer should not connect to itself")
		return false
	}
	logger := n.logger.With(zap.String("peer", id.String()))
	ua, found := n.getUserAgentOfPeer(id)
	if !found {
		logger.Warn("missing user agent for peer")
		return false
	}
	if nodeType := ua.NodeType(); nodeType == Exporter.String() {
		logger.Debug("found exporter peer")
		return true
	}
	pk := ua.NodePubKeyHash()
	if len(pk) == 0 {
		logger.Debug("missing public key hash for peer")
		return false
	}
	// lookup by pk hash
	return n.lookupHandler(pk)
}

// connectNode tries to connect to the given node, returns whether the node is valid and error
func (n *p2pNetwork) connectNode(node *enode.Node) (*peer.AddrInfo, error) {
	info, err := convertToAddrInfo(node)
	if err != nil {
		return nil, errors.Wrap(err, "could not convert node to peer info")
	}
	if n.host.Network().Connectedness(info.ID) == network.Connected {
		return info, nil
	}
	if err := n.connectWithPeer(n.ctx, *info); err != nil {
		return info, errors.Wrap(err, "could not connect with peer")
	}
	return info, nil
}

// tryNode tries to connect to the given node if they share committees
func (n *p2pNetwork) tryNode(node *enode.Node) {
	// trying to get the public key hash from the ENR, and thus identify operators w/o connection
	pkh, err := extractOperatorPubKeyHashEntry(node.Record())
	if err != nil {
		n.logger.Warn("could not extract operator public key", zap.Error(err))
	}
	if pkh != nil {
		logger := n.logger.With(zap.String("pkh", string(*pkh)))
		shouldConnect := n.lookupHandler != nil && n.lookupHandler(string(*pkh))
		logger.Debug("found public key hash entry", zap.Bool("shouldConnect", shouldConnect))
		if shouldConnect {
			if info, err := n.connectNode(node); err != nil {
				logger.Warn("can't connect to node")
			} else if info != nil {
				logger.Debug("forced connection by ENR", zap.String("info", info.ID.String()))
			}
		}
		return
	}
	// TODO: identify exporters by other means, as they don't have a public key hash entry

	// if `pkh` was not found in the node's ENR -> try with the values from user agent
	// TODO: remove once enough operators are on >=v0.1.9 where the ENR entry (`pkh`) was be added
	n.tryNodeByUserAgent(node)
}

// tryNodeByUserAgent try to connect to the given node by its user agent
func (n *p2pNetwork) tryNodeByUserAgent(node *enode.Node) {
	pi, err := n.connectNode(node)
	if err != nil {
		n.logger.Debug("can't connect to node")
		return
	}
	logger := n.logger.With(zap.String("info", pi.String()))
	ctx, cancel := context.WithTimeout(n.ctx, time.Second*5)
	defer cancel()
	if connected := n.waitUntilConnected(ctx, pi.ID); !connected {
		logger.Warn("not connected")
		return
	}
	if n.shouldConnectByUserAgent(pi.ID) {
		logger.Debug("forced connection by user agent")
		return
	}
	// otherwise -> disconnect
	if err = n.host.Network().ClosePeer(pi.ID); err != nil {
		logger.Warn("can't close connection", zap.Error(err))
		return
	}
	logger.Debug("closed connection as peer is irrelevant")
}

// waitUntilConnected blocks until the peer is connected or context cancelled/timed-out
// TODO: 	implement using events (e.g. n.host.EventBus() or events.Feed own by peersIndex)
// 			we should wait until indexed by network notifee, instead of sleeping
func (n *p2pNetwork) waitUntilConnected(ctx context.Context, id peer.ID) bool {
	for {
		if n.host.Network().Connectedness(id) == network.Connected {
			time.Sleep(time.Millisecond * 50) // give it enough time to get indexed by peersIndex
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		time.Sleep(time.Millisecond * 500)
	}
}

// dv5Logger implements log.Handler to track logs of discv5
type dv5Logger struct {
	logger *zap.Logger
}

// Log takes a record and uses the zap.Logger to print it
func (dvl *dv5Logger) Log(r *log.Record) error {
	logger := dvl.logger.With(zap.Any("context", r.Ctx))
	switch r.Lvl {
	case log.LvlTrace:
		logger.Debug(r.Msg)
	case log.LvlDebug:
		logger.Debug(r.Msg)
	case log.LvlInfo:
		logger.Info(r.Msg)
	case log.LvlWarn:
		logger.Warn(r.Msg)
	case log.LvlError:
		logger.Error(r.Msg)
	case log.LvlCrit:
		logger.Fatal(r.Msg)
	default:
	}
	return nil
}
