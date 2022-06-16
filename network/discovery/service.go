package discovery

import (
	"context"
	"github.com/bloxapp/ssv/network/peers"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
	"io"
)

const (
	//udp4 = "udp4"
	//udp6 = "udp6"
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
	Logger *zap.Logger

	Host        host.Host
	DiscV5Opts  *DiscV5Options
	ConnIndex   peers.ConnectionIndex
	HostAddress string
	HostDNS     string

	ForkVersion forksprotocol.ForkVersion
}

// Service is the interface for discovery
type Service interface {
	discovery.Discovery
	io.Closer
	RegisterSubnets(subnets ...int64) error
	DeregisterSubnets(subnets ...int64) error
	Bootstrap(handler HandleNewPeer) error
	UpdateForkVersion(forkv forksprotocol.ForkVersion) error
}

// NewService creates new discovery.Service
func NewService(ctx context.Context, opts Options) (Service, error) {
	if opts.DiscV5Opts == nil {
		return NewLocalDiscovery(ctx, opts.Logger, opts.Host)
	}
	return newDiscV5Service(ctx, &opts)
}
