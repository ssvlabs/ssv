package discovery

import (
	"context"
	"fmt"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	mdnsDiscover "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
)

const (
	// LocalDiscoveryServiceTag is used in our mDNS advertisements to discover other peers
	LocalDiscoveryServiceTag = "ssv.discovery"
)

// localDiscovery implements ssv_discovery.Service using mDNS and KAD-DHT
type localDiscovery struct {
	ctx        context.Context
	svc        mdnsDiscover.Service
	disc       discovery.Discovery
	routingTbl routing.Routing

	host host.Host
}

// NewLocalDiscovery creates an mDNS discovery service and attaches it to the libp2p Host.
// This lets us automatically discover peers on the same LAN and connect to them.
// An empty serviceTag falls back to LocalDiscoveryServiceTag; tests use a unique tag
// to isolate concurrently-running mDNS groups.
func NewLocalDiscovery(ctx context.Context, logger *zap.Logger, host host.Host, serviceTag string) (Service, error) {
	logger = logger.Named(log.NameDiscoveryService)
	logger.Debug("configuring mdns")

	if serviceTag == "" {
		serviceTag = LocalDiscoveryServiceTag
	}

	routingDHT, disc, err := NewKadDHT(ctx, host, dht.ModeServer)
	if err != nil {
		return nil, fmt.Errorf("could not create DHT: %w", err)
	}

	return &localDiscovery{
		ctx:        ctx,
		host:       host,
		routingTbl: routingDHT,
		disc:       disc,
		svc: mdnsDiscover.NewMdnsService(host, serviceTag, &discoveryNotifee{
			handler: handle(host, func(e PeerEvent) {
				err := host.Connect(ctx, e.AddrInfo)
				if err != nil {
					logger.Warn("could not connect to peer", zap.Any("addrInfo", e.AddrInfo), zap.Error(err))
					return
				}
				logger.Debug("connected new peer", zap.Any("addrInfo", e.AddrInfo))
			}),
		}),
	}, nil
}

func handle(host host.Host, handler HandleNewPeer) HandleNewPeer {
	return func(e PeerEvent) {
		ctns := host.Network().Connectedness(e.AddrInfo.ID)
		switch ctns {
		case libp2pnetwork.Connected:
		default:
			go handler(e)
		}
	}
}

// Bootstrap starts to listen to new nodes
func (md *localDiscovery) Bootstrap(handler HandleNewPeer) error {
	err := md.svc.Start()
	if err != nil {
		return fmt.Errorf("could not start mdns service: %w", err)
	}
	return md.routingTbl.Bootstrap(md.ctx)
}

// Advertise implements discovery.Advertiser
func (md *localDiscovery) Advertise(ctx context.Context, ns string, opt ...discovery.Option) (time.Duration, error) {
	return md.disc.Advertise(ctx, ns, opt...)
}

// FindPeers implements discovery.Discoverer
func (md *localDiscovery) FindPeers(ctx context.Context, ns string, opt ...discovery.Option) (<-chan peer.AddrInfo, error) {
	return md.disc.FindPeers(ctx, ns, opt...)
}

// RegisterSubnets implements Service
func (md *localDiscovery) RegisterSubnets(subnets ...uint64) (updated bool, err error) {
	// TODO
	return false, nil
}

// DeregisterSubnets implements Service
func (md *localDiscovery) DeregisterSubnets(subnets ...uint64) (updated bool, err error) {
	// TODO
	return false, nil
}

func (md *localDiscovery) PublishENR() {
	// TODO
}

// DiscoveryStale implements Service. Local discovery has no discv5 socket to
// wedge, so it's never stale.
func (md *localDiscovery) DiscoveryStale(time.Duration) bool {
	return false
}

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	handler HandleNewPeer
}

// discoveryNotifee implements mdnsDiscover.Notifee
var _ mdnsDiscover.Notifee = &discoveryNotifee{}

// HandlePeerFound connects to peers discovered via mDNS. Once they're connected,
// the PubSub system will automatically start interacting with them if they also
// support PubSub.
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	n.handler(PeerEvent{AddrInfo: pi})
}

func (md *localDiscovery) Close() error {
	if err := md.svc.Close(); err != nil {
		return err
	}
	return nil
}

func (dvs *localDiscovery) UpdateDomainType(domain spectypes.DomainType) error {
	// TODO
	return nil
}
