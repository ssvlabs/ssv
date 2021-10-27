package p2p

import (
	"bytes"
	"context"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/herumi/bls-eth-go-binary/bls"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	mdnsDiscover "github.com/libp2p/go-libp2p/p2p/discovery"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers/scorers"
	"go.opencensus.io/trace"
	"net"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	maxPeers = 1000
	udp4     = "udp4"
	udp6     = "udp6"
	tcp      = "tcp"
)

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	host   host.Host
	logger *zap.Logger
}

// HandlePeerFound connects to peers discovered via mDNS. Once they're connected,
// the PubSub system will automatically start interacting with them if they also
// support PubSub.
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	err := n.host.Connect(context.Background(), pi)
	if err != nil {
		n.logger.Error("error connecting to peer", zap.String("peer_id", pi.ID.Pretty()), zap.Error(err))
	}
}

// setupMdnsDiscovery creates an mDNS discovery service and attaches it to the libp2p Host.
// This lets us automatically discover peers on the same LAN and connect to them.
func setupMdnsDiscovery(ctx context.Context, logger *zap.Logger, host host.Host) error {
	disc, err := mdnsDiscover.NewMdnsService(ctx, host, DiscoveryInterval, DiscoveryServiceTag)
	if err != nil {
		return errors.Wrap(err, "failed to create new mDNS service")
	}

	disc.RegisterNotifee(&discoveryNotifee{
		host:   host,
		logger: logger,
	})

	return nil
}

func setupDiscV5(ctx context.Context, n *p2pNetwork) error {
	n.peers = peers.NewStatus(ctx, &peers.StatusConfig{
		PeerLimit: maxPeers,
		ScorerParams: &scorers.Config{
			BadResponsesScorerConfig: &scorers.BadResponsesScorerConfig{
				Threshold:     5,
				DecayInterval: time.Hour,
			},
		},
	})

	listener, err := n.createDiscV5Listener()
	if err != nil {
		return errors.Wrap(err, "failed to create discv5 listener")
	}
	record := listener.Self()
	n.logger.Info("ENR", zap.String("enr", record.String()))
	n.dv5Listener = listener

	err = n.connectToBootnodes()
	if err != nil {
		return errors.Wrap(err, "could not add bootnode to the exclusion list")
	}
	go n.iteratePeers(n.dv5Listener.RandomNodes())

	return nil
}

func (n *p2pNetwork) networkNotifiee(reconnect bool) *libp2pnetwork.NotifyBundle {
	return &libp2pnetwork.NotifyBundle{
		ConnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			logger := n.logger
			if conn != nil {
				if conn.RemoteMultiaddr() != nil {
					logger = logger.With(zap.String("multiaddr", conn.RemoteMultiaddr().String()))
				}
				if len(conn.RemotePeer()) > 0 {
					logger = logger.With(zap.String("peerID", conn.RemotePeer().String()))
				}
				logger.Debug("connected peer")
			}
		},
		DisconnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			logger := n.logger
			if conn != nil {
				ai := peer.AddrInfo{Addrs: []ma.Multiaddr{}}
				if conn.RemoteMultiaddr() != nil {
					addr := conn.RemoteMultiaddr()
					logger = logger.With(zap.String("multiaddr", addr.String()))
					ai.Addrs = []ma.Multiaddr{addr}
				}
				if len(conn.RemotePeer()) > 0 {
					p := conn.RemotePeer()
					logger = logger.With(zap.String("peerID", p.String()))
					ai.ID = p
				}
				logger.Debug("disconnected peer")
				if reconnect {
					go n.reconnect(logger, ai)
				}
			}
		},
		//ClosedStreamF: func(n network.Network, stream network.Stream) {
		//
		//},
		//OpenedStreamF: func(n network.Network, stream network.Stream) {
		//
		//},
	}
}

func (n *p2pNetwork) connectToBootnodes() error {
	nodes := make([]*enode.Node, 0, len(n.cfg.Discv5BootStrapAddr))
	for _, addr := range n.cfg.Discv5BootStrapAddr {
		bootNode, err := enode.Parse(enode.ValidSchemes, addr)
		if err != nil {
			return err
		}
		// do not dial bootnodes with their tcp ports not set
		if err := bootNode.Record().Load(enr.WithEntry(tcp, new(enr.TCP))); err != nil {
			if !enr.IsNotFound(err) {
				n.logger.Error("Could not retrieve tcp port", zap.Error(err))
			}

			n.logger.Error("Could not retrieve tcp port", zap.Error(err))
			continue
		}
		nodes = append(nodes, bootNode)
	}
	multiAddresses := convertToMultiAddr(n.logger, nodes)
	n.connectWithAllPeers(multiAddresses)
	return nil
}

func (n *p2pNetwork) listenUDP(ipAddr net.IP) (*net.UDPConn, error) {
	// BindIP is used to specify the ip
	// on which we will bind our listener on
	// by default we will listen to all interfaces.
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
	return conn, nil
}

func (n *p2pNetwork) createDiscV5Listener() (*discover.UDPv5, error) {
	privKey := n.privKey
	ipAddr := n.ipAddr()
	conn, err := n.listenUDP(ipAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to UDP")
	}
	localNode, err := n.createExtendedLocalNode()
	if err != nil {
		return nil, errors.Wrap(err, "could not create local node")
	}
	dv5Cfg := discover.Config{
		PrivateKey: privKey,
	}

	dv5Cfg.Bootnodes, err = parseDiscV5Addrs(n.cfg.Discv5BootStrapAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not read bootstrap addresses")
	}

	listener, err := discover.ListenV5(conn, localNode, dv5Cfg)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to discV5")
	}
	return listener, nil
}

func (n *p2pNetwork) connectWithAllPeers(multiAddrs []ma.Multiaddr) {
	addrInfos, err := peer.AddrInfosFromP2pAddrs(multiAddrs...)
	if err != nil {
		n.logger.Error("Could not convert to peer address info's from multiaddresses", zap.Error(err))
		return
	}
	for _, info := range addrInfos {
		// make each dial non-blocking
		go func(info peer.AddrInfo) {
			if err := n.connectWithPeer(n.ctx, info); err != nil {
				//log.Print("Could not connect with peer ", info.String(), err)
				//log.WithError(err).Tracef("Could not connect with peer %s", info.String()) TODO need to add log with trace level
			}
		}(info)
	}
}

func (n *p2pNetwork) connectWithPeer(ctx context.Context, info peer.AddrInfo) error {
	ctx, span := trace.StartSpan(ctx, "p2p.connectWithPeer")
	defer span.End()

	if info.ID == n.host.ID() {
		n.logger.Debug("could not connect to current/self peer")
		return nil
	}
	n.logger.Debug("connecting to peer", zap.String("peerID", info.ID.String()))

	if n.peers.IsBad(info.ID) {
		n.logger.Warn("bad peer", zap.String("peerID", info.ID.String()))
		return errors.New("refused to connect to bad peer")
	}
	if n.host.Network().Connectedness(info.ID) == libp2pnetwork.Connected {
		n.logger.Debug("peer is already connected", zap.String("peerID", info.ID.String()))
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := n.host.Connect(ctx, info); err != nil {
		n.logger.Warn("failed to connect to peer", zap.String("peerID", info.ID.String()), zap.Error(err))
		return err
	}
	n.logger.Debug("connected to peer", zap.String("peerID", info.ID.String()))

	return nil
}

// This checks our set max peers in our config, and
// determines whether our currently connected and
// active peers are above our set max peer limit.
func (n *p2pNetwork) isPeerAtLimit() bool {
	numOfConns := len(n.host.Network().Peers())
	activePeers := len(n.peers.Active())
	return activePeers >= maxPeers || numOfConns >= maxPeers
}

// FindPeers finds new peers  watches for new nodes in the network and adds them to the peerstore.
func (n *p2pNetwork) FindPeers(pubkeys ...*bls.PublicKey) {
	iterator := n.dv5Listener.RandomNodes()
	iterator = enode.Filter(iterator, filterPeerByOperatorsPubKey(n.filterPeer, pubkeys...))
	n.iteratePeers(iterator)
}

// iteratePeers accepts some iterator and loop it for new peers
func (n *p2pNetwork) iteratePeers(iterator enode.Iterator) {
	defer iterator.Close()
	for {
		// Exit if service's context is canceled
		if n.ctx.Err() != nil {
			break
		}
		if n.isPeerAtLimit() {
			// Pause the main loop for a period to stop looking
			// for new peers.
			n.logger.Debug("at peer limit")
			time.Sleep(6 * time.Second)
			continue
		}
		exists := iterator.Next()
		if !exists {
			break
		}
		node := iterator.Node()
		peerInfo, _, err := convertToAddrInfo(node)
		if err != nil {
			n.logger.Debug("could not convert to peer info", zap.String("err", err.Error()))
			continue
		}
		go func(info *peer.AddrInfo) {
			if err := n.connectWithPeer(n.ctx, *info); err != nil {
				//n.logger.Debug("could not connect with peer", zap.String("err", err.Error()))
			}
		}(peerInfo)
	}
}

type peerFilter func(node *enode.Node) bool

// filterPeer validates each node that we retrieve from our dht. We
// try to ascertain that the peer can be a valid protocol peer.
// Validity Conditions:
// 1) The local node is still actively looking for peers to
//    connect to.
// 2) Peer has a valid IP and TCP port set in their enr.
// 3) Peer hasn't been marked as 'bad'
// 4) Peer is not currently active or connected.
// 5) Peer is ready to receive incoming connections.
// --6) Peer's fork digest in their ENR matches that of
// 	  our localnodes.
func (n *p2pNetwork) filterPeer(node *enode.Node) bool {
	// Ignore nil or nodes with no ip address stored
	if node == nil || node.IP() == nil {
		return false
	}
	// do not dial nodes with their tcp ports not set
	if err := node.Record().Load(enr.WithEntry("tcp", new(enr.TCP))); err != nil {
		if !enr.IsNotFound(err) {
			n.logger.Debug("could not retrieve tcp port", zap.Error(err))
		}
		return false
	}
	peerData, multiAddr, err := convertToAddrInfo(node)
	if err != nil {
		n.logger.Debug("could not convert to peer data", zap.Error(err))
		return false
	}
	if n.peers.IsBad(peerData.ID) {
		return false
	}
	if n.peers.IsActive(peerData.ID) {
		return false
	}
	if n.host.Network().Connectedness(peerData.ID) == libp2pnetwork.Connected {
		return false
	}
	if !n.peers.IsReadyToDial(peerData.ID) {
		return false
	}
	nodeENR := node.Record()
	// Decide whether or not to connect to peer that does not
	// match the proper fork ENR data with our local node.
	//if s.genesisValidatorsRoot != nil {
	//	if err := s.compareForkENR(nodeENR); err != nil {
	//		log.WithError(err).Trace("Fork ENR mismatches between peer and local node")
	//		return false
	//	}
	//}
	// Add peer to peer handler.
	n.peers.Add(nodeENR, peerData.ID, multiAddr, libp2pnetwork.DirUnknown)
	return true
}

// filterPeerByOperatorsPubKey filters peers specifically for a particular operators
func filterPeerByOperatorsPubKey(baseFilter peerFilter, pubkeys ...*bls.PublicKey) func(node *enode.Node) bool {
	var pks [][]byte
	for _, pubkey := range pubkeys {
		pks = append(pks, []byte(pubKeyHash(pubkey.SerializeToHexStr())))
	}
	return func(node *enode.Node) bool {
		if baseFilter != nil && !baseFilter(node) {
			return false
		}
		pkEntry, err := extractOperatorPubKeyEntry(node.Record())
		if err != nil {
			return false
		}
		pkEntryVal := pkEntry[:8]
		// lookup the public key from record with all given public keys
		for _, pk := range pks {
			if bytes.Index(pk, pkEntryVal) == 0 {
				return true
			}
		}
		return false
	}
}
