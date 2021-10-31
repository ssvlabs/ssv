package p2p

import (
	"bytes"
	"context"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	mdnsDiscover "github.com/libp2p/go-libp2p/p2p/discovery"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers/scorers"
	"go.opencensus.io/trace"
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

// setupDiscV5 create a discv5 service and starts it
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

	listener, localNode, err := n.createDiscV5Listener()
	if err != nil {
		return errors.Wrap(err, "failed to create discv5 listener")
	}
	record := listener.Self()
	n.logger.Info("ENR", zap.String("enr", record.String()))
	n.dv5Listener = listener
	n.eNode = localNode

	err = n.connectToBootnodes()
	if err != nil {
		return errors.Wrap(err, "could not connect to bootnodes")
	}

	return nil
}

// findNetworkPeersLoop will keep looking for peers in the network while the main context is not done
func (n *p2pNetwork) findNetworkPeersLoop(interval time.Duration) {
	for {
		select {
		case <-n.ctx.Done():
			return
		default:
			n.findNetworkPeers(interval)
		}
	}
}

// findNetworkPeers finds peers in the network with the given timeout
func (n *p2pNetwork) findNetworkPeers(timeout time.Duration) {
	logger := n.logger.With(zap.String("who", "findNetworkPeers"))
	logger.Debug("finding network peers...")
	// first connecting to bootnodes
	if err := n.connectToBootnodes(); err != nil {
		logger.Error("could not connect to bootnodes", zap.Error(err))
	}
	ctx, cancel := context.WithTimeout(n.ctx, timeout)
	defer cancel()
	iterator := enode.Filter(n.dv5Listener.RandomNodes(), n.filterPeer)
	n.iteratePeers(ctx, iterator, func(info *peer.AddrInfo, node *enode.Node) {
		if info == nil {
			return
		}
		logger.Debug("found peer in random search", zap.String("peer", info.String()))
		if err := n.connectWithPeer(ctx, *info); err != nil {
			return
		}
		//if _, multiAddr, err := convertToAddrInfo(node); err == nil {
		//	n.peers.Add(node.Record(), info.ID, multiAddr, libp2pnetwork.DirOutbound)
		//}
		//if err := n.eNode.Database().UpdateNode(node); err != nil {
		//	n.logger.Debug("could not update peer in DB", zap.String("peer", info.String()),
		//		zap.String("err", err.Error()))
		//} else {
		//	n.logger.Debug("enode updated", zap.String("peer", info.String()))
		//}
	})
}

// networkNotifiee notifies on network events
func (n *p2pNetwork) networkNotifiee(reconnect bool) *libp2pnetwork.NotifyBundle {
	_logger := n.logger.With(zap.String("who", "networkNotifiee"))
	return &libp2pnetwork.NotifyBundle{
		ConnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			go func() {
				n.peers.Add(new(enr.Record), conn.RemotePeer(), conn.RemoteMultiaddr(), conn.Stat().Direction)
				n.peers.SetConnectionState(conn.RemotePeer(), peers.PeerConnecting)
				logger := _logger.With(zap.String("conn", conn.ID()),
					zap.String("multiaddr", conn.RemoteMultiaddr().String()),
					zap.String("peerID", conn.RemotePeer().String()))
				// trying to open a stream
				if s, err := conn.NewStream(context.Background()); err != nil {
					n.peers.SetConnectionState(conn.RemotePeer(), peers.PeerDisconnected)
					logger.Warn("failed to open stream", zap.Error(err))
					return
				} else if err = s.Close(); err != nil {
					logger.Warn("failed to close stream", zap.Error(err))
				}
				n.peers.SetConnectionState(conn.RemotePeer(), peers.PeerConnected)
				logger.Debug("connected peer")
			}()
		},
		DisconnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil {
				return
			}
			go func() {
				logger := _logger.With(zap.String("conn", conn.ID()),
					zap.String("multiaddr", conn.RemoteMultiaddr().String()),
					zap.String("peerID", conn.RemotePeer().String()))
				ai := peer.AddrInfo{Addrs: []ma.Multiaddr{conn.RemoteMultiaddr()}, ID: conn.RemotePeer()}
				logger.Debug("disconnected peer")
				n.peers.SetConnectionState(conn.RemotePeer(), peers.PeerDisconnected)
				if reconnect {
					go n.reconnect(logger, ai)
				}
			}()
		},
	}
}

// connectToBootnodes connects to all configured bootnodes
func (n *p2pNetwork) connectToBootnodes() error {
	nodes, err := n.bootnodes()
	if err != nil {
		return err
	}
	multiAddresses := convertToMultiAddr(n.logger, nodes)
	n.connectWithPeers(multiAddresses)
	return nil
}

// connectWithPeers connects with the given peers
func (n *p2pNetwork) connectWithPeers(multiAddrs []ma.Multiaddr) {
	addrInfos, err := peer.AddrInfosFromP2pAddrs(multiAddrs...)
	if err != nil {
		n.logger.Error("Could not convert to peer address info's from multiaddresses", zap.Error(err))
		return
	}
	for _, info := range addrInfos {
		// make each dial non-blocking
		go func(info peer.AddrInfo) {
			if err := n.connectWithPeer(n.ctx, info); err != nil {
				//n.logger.Debug("could not connect to peer", zap.String("err", err.Error()))
				return
			}
		}(info)
	}
}

// connectWithPeer connects with the given peer
func (n *p2pNetwork) connectWithPeer(ctx context.Context, info peer.AddrInfo) error {
	ctx, span := trace.StartSpan(ctx, "p2p.connectWithPeer")
	defer span.End()

	if info.ID == n.host.ID() {
		n.logger.Debug("could not connect to current/self peer")
		return nil
	}
	logger := n.logger.With(zap.String("peer", info.String()))
	logger.Debug("connecting to peer")

	if n.peers.IsBad(info.ID) {
		logger.Warn("bad peer")
		return errors.New("refused to connect to bad peer")
	}
	if n.host.Network().Connectedness(info.ID) == libp2pnetwork.Connected {
		logger.Debug("peer is already connected")
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := n.host.Connect(ctx, info); err != nil {
		logger.Warn("could not connect to peer", zap.Error(err))
		return err
	}

	logger.Debug("connected to peer", zap.String("peer", info.String()))

	return nil
}

// isPeerAtLimit determines whether the current peer count has reached the limit
func (n *p2pNetwork) isPeerAtLimit() bool {
	numOfConns := len(n.host.Network().Peers())
	activePeers := len(n.peers.Active())
	return activePeers >= maxPeers || numOfConns >= maxPeers
}

// FindPeers finds new peers watches for new nodes in the network and adds them to the peerstore.
func (n *p2pNetwork) FindPeers(ctx context.Context, operatorsPubKeys ...[]byte) {
	if len(operatorsPubKeys) == 0 {
		return
	}
	logger := n.logger.With(zap.String("who", "FindPeers"))
	var pks []string
	for _, opk := range operatorsPubKeys {
		pks = append(pks, string(opk))
	}
	logger.Debug("finding operators...", zap.Any("pks", pks))
	iterator := n.dv5Listener.RandomNodes()
	iterator = enode.Filter(iterator, filterPeerByOperatorsPubKey(n.filterPeer, operatorsPubKeys...))
	n.iteratePeers(ctx, iterator, func(info *peer.AddrInfo, node *enode.Node) {
		if info == nil {
			return
		}
		logger.Debug("found peer", zap.String("peer", info.String()))
		// ignore error which is printed in connectWithPeer
		_ = n.connectWithPeer(ctx, *info)
	})
}

// iteratePeers accepts some iterator and loop it for new peers
func (n *p2pNetwork) iteratePeers(ctx context.Context, iterator enode.Iterator, handler func(info *peer.AddrInfo, node *enode.Node)) {
	defer iterator.Close()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// Exit if service's context is canceled
		//if n.ctx.Err() != nil {
		//	break
		//}
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
		go handler(peerInfo, node)
	}
}

// peerFilter is an interface for discovery filter
type peerFilter func(node *enode.Node) bool

// filterPeer filters nodes based on the following rules
// 	- peer has a valid IP and TCP port set in their enr
//  - peer hasn't been marked as 'bad'
//  - peer is not currently active or connected
//  - peer is ready to receive incoming connections.
func (n *p2pNetwork) filterPeer(node *enode.Node) bool {
	// Ignore nil or nodes with no ip address stored
	if node == nil || node.IP() == nil {
		return false
	}
	logger := n.logger.With(zap.String("who", "filterPeer"))
	// do not dial nodes with their tcp ports not set
	if err := node.Record().Load(enr.WithEntry("tcp", new(enr.TCP))); err != nil {
		if !enr.IsNotFound(err) {
			logger.Debug("could not retrieve tcp port", zap.Error(err))
		}
		return false
	}
	peerData, multiAddr, err := convertToAddrInfo(node)
	if err != nil {
		logger.Debug("could not convert to peer data", zap.Error(err))
		return false
	}
	logger = logger.With(zap.String("peer", peerData.String()))
	if n.peers.IsBad(peerData.ID) {
		logger.Debug("filtered bad peer")
		return false
	}
	if n.peers.IsActive(peerData.ID) {
		logger.Debug("filtered inactive peer")
		return false
	}
	if n.host.Network().Connectedness(peerData.ID) == libp2pnetwork.Connected {
		logger.Debug("filtered connected peer")
		return false
	}
	if !n.peers.IsReadyToDial(peerData.ID) {
		logger.Debug("filtered unreachable peer")
		return false
	}
	nodeENR := node.Record()

	// TODO check regarding fork validation

	// Add peer to peer handler.
	n.peers.Add(nodeENR, peerData.ID, multiAddr, libp2pnetwork.DirUnknown)
	logger.Debug("peer is valid")

	return true
}

// filterPeerByOperatorsPubKey filters peers specifically for a particular operators
func filterPeerByOperatorsPubKey(baseFilter peerFilter, pks ...[]byte) func(node *enode.Node) bool {
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
