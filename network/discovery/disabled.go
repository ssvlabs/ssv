package discovery

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Disabled is the Service of a node that runs no peer discovery: it keeps only the peers it dials itself
// (TrustedPeers, or a test harness wiring a mesh directly) and the peers that dial it. Subnet registration
// and ENR publishing have nothing to update, so they are no-ops.
type Disabled struct{}

var _ Service = Disabled{}

// Bootstrap implements Service; there is nothing to start, and no peer is ever handed to handler.
func (Disabled) Bootstrap(HandleNewPeer) error { return nil }

// Advertise implements discovery.Advertiser as a no-op.
func (Disabled) Advertise(context.Context, string, ...discovery.Option) (time.Duration, error) {
	return 0, nil
}

// FindPeers implements discovery.Discoverer: the channel is closed without a peer on it.
func (Disabled) FindPeers(context.Context, string, ...discovery.Option) (<-chan peer.AddrInfo, error) {
	peers := make(chan peer.AddrInfo)
	close(peers)
	return peers, nil
}

// RegisterSubnets implements Service as a no-op.
func (Disabled) RegisterSubnets(...uint64) (updated bool, err error) { return false, nil }

// DeregisterSubnets implements Service as a no-op.
func (Disabled) DeregisterSubnets(...uint64) (updated bool, err error) { return false, nil }

// PublishENR implements Service as a no-op.
func (Disabled) PublishENR() {}

// Close implements io.Closer; there is nothing to release.
func (Disabled) Close() error { return nil }
