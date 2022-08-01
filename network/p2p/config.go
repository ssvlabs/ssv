package p2pv1

import (
	"crypto/ecdsa"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/forks"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	uc "github.com/bloxapp/ssv/utils/commons"
)

const (
	minPeersBuffer = 10
)

// Config holds the configuration options for p2p network
type Config struct {
	// prod enr
	Bootnodes string `yaml:"Bootnodes" env:"BOOTNODES" env-description:"Bootnodes to use to start discovery, seperated with ';'" env-default:"enr:-LK4QMmL9hLJ1csDN4rQoSjlJGE2SvsXOETfcLH8uAVrxlHaELF0u3NeKCTY2eO_X1zy5eEKcHruyaAsGNiyyG4QWUQBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g"`

	TCPPort     int    `yaml:"TcpPort" env:"TCP_PORT" env-default:"13001" env-description:"TCP port for p2p transport"`
	UDPPort     int    `yaml:"UdpPort" env:"UDP_PORT" env-default:"12001" env-description:"UDP port for discovery"`
	HostAddress string `yaml:"HostAddress" env:"HOST_ADDRESS" env-description:"External ip node is exposed for discovery"`
	HostDNS     string `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS node is exposed for discovery"`

	RequestTimeout   time.Duration `yaml:"RequestTimeout" env:"P2P_REQUEST_TIMEOUT"  env-default:"7s"`
	MaxBatchResponse uint64        `yaml:"MaxBatchResponse" env:"P2P_MAX_BATCH_RESPONSE" env-default:"25" env-description:"Maximum number of returned objects in a batch"`
	MaxPeers         int           `yaml:"MaxPeers" env:"P2P_MAX_PEERS" env-default:"60" env-description:"Connected peers limit for connections"`
	TopicMaxPeers    int           `yaml:"TopicMaxPeers" env:"P2P_TOPIC_MAX_PEERS" env-default:"5" env-description:"Connected peers limit per pubsub topic"`

	// Subnets is a static bit list of subnets that this node will register upon start.
	Subnets string `yaml:"Subnets" env:"SUBNETS" env-description:"Hex string that represents the subnets that this node will join upon start"`
	// PubSubScoring is a flag to turn on/off pubsub scoring
	PubSubScoring bool `yaml:"PubSubScoring" env:"PUBSUB_SCORING" env-description:"Flag to turn on/off pubsub scoring"`
	// P2pLog is a flag to turn on/off network logs
	P2pLog bool `yaml:"P2pLog" env:"P2P_LOG" env-description:"Flag to turn on/off network debug logs"`
	// PubSubTrace is a flag to turn on/off pubsub tracing in logs
	PubSubTrace bool `yaml:"PubSubTrace" env:"PUBSUB_TRACE" env-description:"Flag to turn on/off pubsub tracing in logs"`
	// DiscoveryTrace is a flag to turn on/off discovery tracing in logs
	DiscoveryTrace bool `yaml:"DiscoveryTrace" env:"DISCOVERY_TRACE" env-description:"Flag to turn on/off discovery tracing in logs"`
	// NetworkID is the network of this node
	NetworkID string `yaml:"NetworkID" env:"NETWORK_ID" env-default:"ssv-testnet-v2" env-description:"Network ID is the network of this node"`
	// NetworkPrivateKey is used for network identity, MUST be injected
	NetworkPrivateKey *ecdsa.PrivateKey
	// OperatorPublicKey is used for operator identity, optional
	OperatorID string
	// Router propagate incoming network messages to the responsive components
	Router network.MessageRouter
	// UserAgent to use by libp2p identify protocol
	UserAgent string
	// ForkVersion to use
	ForkVersion forksprotocol.ForkVersion
	// Logger to used by network services
	Logger *zap.Logger

	PubsubMsgCacheTTL         time.Duration `yaml:"PubsubMsgCacheTTL" env:"PUBSUB_MSG_CACHE_TTL" env-description:"How long a message ID will be remembered as seen"`
	PubsubOutQueueSize        int           `yaml:"PubsubOutQueueSize" env:"PUBSUB_OUT_Q_SIZE" env-description:"The size that we assign to the outbound pubsub message queue"`
	PubsubValidationQueueSize int           `yaml:"PubsubValidationQueueSize" env:"PUBSUB_VAL_Q_SIZE" env-description:"The size that we assign to the pubsub validation queue"`
	PubsubValidateThrottle    int           `yaml:"PubsubPubsubValidateThrottle" env:"PUBSUB_VAL_THROTTLE" env-description:"The amount of goroutines used for pubsub msg validation"`
}

// Libp2pOptions creates options list for the libp2p host
// these are the most basic options required to start a network instance,
// other options and libp2p components can be configured on top
func (c *Config) Libp2pOptions(fork forks.Fork) ([]libp2p.Option, error) {
	if c.NetworkPrivateKey == nil {
		return nil, errors.New("could not create options w/o network key")
	}
	sk, err := commons.ConvertToInterfacePrivkey(c.NetworkPrivateKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not convert to interface priv key")
	}

	opts := []libp2p.Option{
		libp2p.Identity(sk),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.UserAgent(c.UserAgent),
	}

	opts, err = c.configureAddrs(opts)
	if err != nil {
		return opts, errors.Wrap(err, "could not setup addresses")
	}

	opts = append(opts, libp2p.Security(noise.ID, noise.New))

	opts = fork.AddOptions(opts)

	return opts, nil
}

func (c *Config) configureAddrs(opts []libp2p.Option) ([]libp2p.Option, error) {
	addrs := make([]ma.Multiaddr, 0)
	maZero, err := commons.BuildMultiAddress("0.0.0.0", "tcp", uint(c.TCPPort), "")
	if err != nil {
		return opts, errors.Wrap(err, "could not build multi address for zero address")
	}
	addrs = append(addrs, maZero)
	ipAddr, err := commons.IPAddr()
	if err != nil {
		return opts, errors.Wrap(err, "could not get ip addr")
	}
	if len(c.Bootnodes) > 0 { // not a local node
		maIP, err := commons.BuildMultiAddress(ipAddr.String(), "tcp", uint(c.TCPPort), "")
		if err != nil {
			return opts, errors.Wrap(err, "could not build multi address for zero address")
		}
		addrs = append(addrs, maIP)
	}
	opts = append(opts, libp2p.ListenAddrs(addrs...))

	// AddrFactory for host address if provided
	if c.HostAddress != "" {
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			external, err := commons.BuildMultiAddress(c.HostAddress, "tcp", uint(c.TCPPort), "")
			if err != nil {
				c.Logger.Error("unable to create external multiaddress", zap.Error(err))
			} else {
				addrs = append(addrs, external)
			}
			return addrs
		}))
	}
	// AddrFactory for DNS address if provided
	if c.HostDNS != "" {
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			external, err := ma.NewMultiaddr(fmt.Sprintf("/dns4/%s/tcp/%d", c.HostDNS, c.TCPPort))
			if err != nil {
				c.Logger.Warn("unable to create external multiaddress", zap.Error(err))
			} else {
				addrs = append(addrs, external)
			}
			return addrs
		}))
	}

	return opts, nil
}

// TransformBootnodes converts bootnodes string and convert it to slice
func (c *Config) TransformBootnodes() []string {
	items := strings.Split(c.Bootnodes, ";")
	if len(items) == 0 {
		// STAGE
		// items = append(items, "enr:-LK4QHVq6HEA2KVnAw593SRMqUOvMGlkP8Jb-qHn4yPLHx--cStvWc38Or2xLcWgDPynVxXPT9NWIEXRzrBUsLmcFkUBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDbUHcyJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g")
		// PROD
		// internal ip
		// items = append(items, "enr:-LK4QPbCB0Mw_8ji7D02OwXmqSRZe9wTmitle_cQnECIl-5GBPH9PH__eUpdeiI_t122inm62uTgO9CptbGNLKNId7gBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArsBGGJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g")
		// external ip
		items = append(items, "enr:-LK4QMmL9hLJ1csDN4rQoSjlJGE2SvsXOETfcLH8uAVrxlHaELF0u3NeKCTY2eO_X1zy5eEKcHruyaAsGNiyyG4QWUQBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g")
	}
	return items
}

func userAgent(fromCfg string) string {
	if len(fromCfg) > 0 {
		return fromCfg
	}
	return uc.GetBuildData()
}
