package p2p

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/network/p2p/discovery"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"net"
	"time"
)

const (
	udp4 = "udp4"
	udp6 = "udp6"
	tcp  = "tcp"

	discoveryTypeMdns   = "mdns"
	discoveryTypeDiscv5 = "discv5"
)

// startDiscovery starts the underlying discovery service
func (n *p2pNetwork) startDiscovery() error {
	if n.cfg.DiscoveryType == discoveryTypeMdns {
		// in mdns discovery - do nothing
		return nil
	}

	//if err := n.connectToBootnodes(); err != nil {
	//	return errors.Wrap(err, "could not connect to bootnodes")
	//}
	go n.listenForNewNodes(n.ctx)
	return nil
}

// setupDiscovery configure discovery service according to configured type
func (n *p2pNetwork) setupDiscovery() error {
	if n.cfg.DiscoveryType == discoveryTypeMdns {
		return discovery.SetupMdnsDiscovery(n.ctx, n.logger, n.host)
	}

	listener, err := n.setupDiscV5()
	if err != nil {
		n.logger.Error("Failed to start discovery", zap.Error(err))
		return err
	}
	n.dv5Listener = listener

	if n.cfg.HostAddress != "" {
		a := net.JoinHostPort(n.cfg.HostAddress, fmt.Sprintf("%d", n.cfg.TCPPort))
		if err := checkAddress(a); err != nil {
			n.logger.Debug("failed to check address", zap.String("addr", a), zap.String("err", err.Error()))
		} else {
			n.logger.Debug("address was checked successfully", zap.String("addr", a))
		}
	}

	return err
}

//
//// connectToBootnodes connects to the configured bootnodes
//func (n *p2pNetwork) connectToBootnodes() error {
//	nodes, err := parseENRs(n.cfg.BootnodesENRs, true)
//	if err != nil {
//		return errors.Wrap(err, "failed to parse bootnodes ENRs")
//	}
//	multiAddrs := convertToMultiAddr(n.logger, nodes)
//	addrInfos, err := peer.AddrInfosFromP2pAddrs(multiAddrs...)
//	if err != nil {
//		return errors.Wrap(err, "could not convert multiaddrs to peers info")
//	}
//	for _, info := range addrInfos {
//		go func(info peer.AddrInfo) {
//			if err := n.connectWithPeer(n.ctx, info); err != nil {
//				n.logger.Warn("can't connect to bootnode", zap.String("peerID", info.ID.String()))
//			}
//		}(info)
//	}
//	return nil
//}

func (n *p2pNetwork) connectWithPeer(ctx context.Context, info peer.AddrInfo) error {
	ctx, span := trace.StartSpan(ctx, "p2p.connectWithPeer")
	defer span.End()

	if info.ID == n.host.ID() {
		return nil
	}
	n.trace("connecting to peer", zap.String("peerID", info.ID.String()))

	if n.host.Network().Connectedness(info.ID) == libp2pnetwork.Connected {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := n.host.Connect(ctx, info); err != nil {
		return errors.Wrap(err, "failed to connect to peer")
	}
	n.trace("connected to peer", zap.String("peerID", info.ID.String()))

	return nil
}
