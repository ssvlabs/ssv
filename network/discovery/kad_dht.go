package discovery

import (
	"context"
	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/routing"
	libp2pdisc "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/pkg/errors"
)

const (
	// DHTDiscoveryProtocol is the protocol prefix used for local discovery (routingTbl), in addition to mDNS
	DHTDiscoveryProtocol = "/ssv/discovery/0.0.1"
)

// NewKadDHT creates a new kademlia DHT and a corresponding discovery service
// NOTE: that the caller must bootstrap the routing.Routing instance
func NewKadDHT(ctx context.Context, host host.Host, mode dht.ModeOpt) (routing.Routing, discovery.Discovery, error) {
	kdht, err := dht.New(ctx, host, dht.ProtocolPrefix(DHTDiscoveryProtocol), dht.Mode(mode))
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create DHT")
	}
	return kdht, libp2pdisc.NewRoutingDiscovery(kdht), nil
}
