package discovery

import (
	"context"
	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	mdnsDiscover "github.com/libp2p/go-libp2p/p2p/discovery/mdns_legacy"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	// DiscoveryInterval is how often we re-publish our mDNS records.
	DiscoveryInterval = time.Second
	// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
	DiscoveryServiceTag = "bloxstaking.ssv"
)

// mdnsDiscovery implements ssv_discovery.Service using mDNS service
type mdnsDiscovery struct {
	ctx    context.Context
	logger *zap.Logger
	svc    mdnsDiscover.Service

	peersLock *sync.RWMutex
	peers     map[string]PeerEvent
}

// NewMdnsDiscovery creates an mDNS discovery service and attaches it to the libp2p Host.
// This lets us automatically discover peers on the same LAN and connect to them.
func NewMdnsDiscovery(ctx context.Context, logger *zap.Logger, host host.Host) (Service, error) {
	svc, err := mdnsDiscover.NewMdnsService(ctx, host, DiscoveryInterval, DiscoveryServiceTag)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new mDNS service")
	}
	return &mdnsDiscovery{
		ctx:       ctx,
		logger:    logger,
		svc:       svc,
		peersLock: &sync.RWMutex{},
	}, nil
}

// Bootstrap starts to listen to new nodes
func (md *mdnsDiscovery) Bootstrap(handler HandleNewPeer) error {
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
	return nil
}

// Advertise implements discovery.Advertiser
func (md *mdnsDiscovery) Advertise(ctx context.Context, ns string, opt ...discovery.Option) (time.Duration, error) {
	// won't be implemented
	return time.Hour, nil
}

// FindPeers implements discovery.Discoverer
func (md *mdnsDiscovery) FindPeers(ctx context.Context, ns string, opt ...discovery.Option) (<-chan peer.AddrInfo, error) {
	cn := make(chan peer.AddrInfo, 4)
	go func() {
		for _, e := range md.peers {
			if ctx.Err() != nil {
				return
			}
			cn <- e.AddrInfo
			time.Sleep(time.Millisecond * 200)
		}
	}()
	return cn, nil
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
