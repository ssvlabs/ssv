package discovery

import (
	"context"
	"io"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/peers"
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
	Host        host.Host
	DiscV5Opts  *DiscV5Options
	ConnIndex   peers.ConnectionIndex
	SubnetsIdx  peers.SubnetsIndex
	HostAddress string
	HostDNS     string

	// DomainType is the SSV network domain of the node
	DomainType spectypes.DomainType
}

// Service is the interface for discovery
type Service interface {
	discovery.Discovery
	io.Closer
	RegisterSubnets(logger *zap.Logger, subnets ...int) error
	DeregisterSubnets(logger *zap.Logger, subnets ...int) error
	Bootstrap(logger *zap.Logger, handler HandleNewPeer) error
}

// NewService creates new discovery.Service
func NewService(ctx context.Context, logger *zap.Logger, opts Options) (Service, error) {
	if opts.DiscV5Opts == nil {
		return NewLocalDiscovery(ctx, logger, opts.Host)
	}
	return newDiscV5Service(ctx, logger, &opts)
}
