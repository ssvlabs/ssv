package discovery

import (
	"context"
	"io"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils/ttl"
)

const (
	// udp4 = "udp4"
	// udp6 = "udp6"
	tcp = "tcp"
)

// CheckPeerLimit enables listener to check peers limit
type CheckPeerLimit func() bool

// PeerEvent is the data passed to peer handler
type PeerEvent struct {
	AddrInfo peer.AddrInfo
	Node     *enode.Node
}

// HandleNewPeer is the function interface for handling new peer
type HandleNewPeer func(e PeerEvent)

// Options represents the options passed to create a service
type Options struct {
	Host                host.Host
	DiscV5Opts          *DiscV5Options
	ConnIndex           peers.ConnectionIndex
	SubnetsIdx          peers.SubnetsIndex
	HostAddress         string
	HostDNS             string
	NetworkConfig       networkconfig.NetworkConfig
	DiscoveredPeersPool *ttl.Map[peer.ID, DiscoveredPeer]
	TrimmedRecently     *ttl.Map[peer.ID, struct{}]
}

// Service is the interface for discovery
type Service interface {
	discovery.Discovery
	io.Closer
	RegisterSubnets(logger *zap.Logger, subnets ...uint64) (updated bool, err error)
	DeregisterSubnets(logger *zap.Logger, subnets ...uint64) (updated bool, err error)
	Bootstrap(logger *zap.Logger, handler HandleNewPeer) error
	PublishENR(logger *zap.Logger)
}

// NewService creates new discovery.Service
func NewService(ctx context.Context, logger *zap.Logger, opts Options) (Service, error) {
	if opts.DiscV5Opts == nil {
		return NewLocalDiscovery(ctx, logger, opts.Host)
	}
	return newDiscV5Service(ctx, logger, &opts)
}

type DiscoveredPeer struct {
	peer.AddrInfo

	// Tries keeps track of how many times we tried to connect to this peer.
	Tries int
	// LastTry is the last time we tried to connect to this peer.
	LastTry time.Time
}
