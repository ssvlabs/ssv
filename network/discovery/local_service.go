package discovery

import (
	"context"
	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/routing"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	mdnsDiscover "github.com/libp2p/go-libp2p/p2p/discovery/mdns_legacy"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	// localDiscoveryInterval is how often we re-publish our mDNS records.
	localDiscoveryInterval = time.Second / 2
	// LocalDiscoveryServiceTag is used in our mDNS advertisements to discover other peers
	LocalDiscoveryServiceTag = "ssv.discovery"
)

// localDiscovery implements ssv_discovery.Service using mDNS and KAD-DHT
type localDiscovery struct {
	ctx        context.Context
	logger     *zap.Logger
	svc        mdnsDiscover.Service
	disc       discovery.Discovery
	routingTbl routing.Routing

	peersLock *sync.RWMutex
	peers     map[string]PeerEvent
}

// NewLocalDiscovery creates an mDNS discovery service and attaches it to the libp2p Host.
// This lets us automatically discover peers on the same LAN and connect to them.
func NewLocalDiscovery(ctx context.Context, logger *zap.Logger, host host.Host) (Service, error) {
	logger.Debug("configuring mdns discovery")

	svc, err := mdnsDiscover.NewMdnsService(ctx, host, localDiscoveryInterval, LocalDiscoveryServiceTag)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new mDNS service")
	}

	routingDHT, disc, err := NewKadDHT(ctx, host, dht.ModeServer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create DHT")
	}

	return &localDiscovery{
		ctx:        ctx,
		logger:     logger,
		svc:        svc,
		peers:      make(map[string]PeerEvent),
		peersLock:  &sync.RWMutex{},
		routingTbl: routingDHT,
		disc:       disc,
	}, nil
}

// Bootstrap starts to listen to new nodes
func (md *localDiscovery) Bootstrap(handler HandleNewPeer) error {
	// default handler
	md.svc.RegisterNotifee(&discoveryNotifee{
		handler: func(e PeerEvent) {
			md.peersLock.Lock()
			defer md.peersLock.Unlock()

			id := e.AddrInfo.ID.String()
			if _, exist := md.peers[id]; !exist {
				md.logger.Debug("found new peer", zap.Any("addrInfo", e.AddrInfo))
				md.peers[id] = e
				go handler(e)
			}
		},
	})
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
func (md *localDiscovery) RegisterSubnets(subnets ...int64) error {
	// TODO
	return nil
}

// DeregisterSubnets implements Service
func (md *localDiscovery) DeregisterSubnets(subnets ...int64) error {
	// TODO
	return nil
}

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	handler HandleNewPeer
}

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
	md.peers = nil
	return nil
}
