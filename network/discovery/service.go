package discovery

import (
	"context"
	"fmt"
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
	SSVConfig           *networkconfig.SSVConfig
	DiscoveredPeersPool *ttl.Map[peer.ID, DiscoveredPeer]
	TrimmedRecently     *ttl.Map[peer.ID, struct{}]
}

// Validate checks if the options are valid.
func (o *Options) Validate() error {
	if len(o.HostDNS) > 0 && len(o.HostAddress) > 0 {
		return fmt.Errorf("only one of HostDNS or HostAddress may be set")
	}
	return nil
}

// Service is the interface for discovery
type Service interface {
	discovery.Discovery
	io.Closer
	RegisterSubnets(subnets ...uint64) (updated bool, err error)
	DeregisterSubnets(subnets ...uint64) (updated bool, err error)
	Bootstrap(handler HandleNewPeer) error
	Node(logger *zap.Logger, info peer.AddrInfo) (*enode.Node, error)
	PublishENR()
}

// NewService creates new discovery.Service
func NewService(ctx context.Context, logger *zap.Logger, opts Options) (Service, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}

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
