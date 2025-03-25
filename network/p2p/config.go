package p2pv1

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	uc "github.com/ssvlabs/ssv/utils/commons"
)

const (
	localDiscvery  = "mdns"
	minPeersBuffer = 10
)

// Config holds the configuration options for p2p network
type Config struct {
	Ctx          context.Context
	Bootnodes    string   `yaml:"Bootnodes" env:"BOOTNODES" env-default:"" env-description:"Bootnodes to use for discovery (semicolon-separated ENRs, e.g. 'enr:-abc123;enr:-def456')" `
	Discovery    string   `yaml:"Discovery" env:"P2P_DISCOVERY" env-default:"discv5" env-description:"Discovery protocol to use (discv5, mdns)" `
	TrustedPeers []string `yaml:"TrustedPeers" env:"TRUSTED_PEERS" env-default:"" env-description:"List of peer IDs to always connect to"`

	TCPPort     uint16 `yaml:"TcpPort" env:"TCP_PORT" env-default:"13001" env-description:"TCP port for P2P transport"`
	UDPPort     uint16 `yaml:"UdpPort" env:"UDP_PORT" env-default:"12001" env-description:"UDP port for discovery"`
	HostAddress string `yaml:"HostAddress" env:"HOST_ADDRESS" env-description:"External IP address for discovery (can be overridden by HostDNS)"`
	HostDNS     string `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS name for discovery (overrides HostAddress if both are specified)"`

	RequestTimeout   time.Duration `yaml:"RequestTimeout" env:"P2P_REQUEST_TIMEOUT"  env-default:"10s" env-description:"Timeout for P2P requests"`
	MaxBatchResponse uint64        `yaml:"MaxBatchResponse" env:"P2P_MAX_BATCH_RESPONSE" env-default:"25" env-description:"Maximum number of objects returned in a batch response"`

	MaxPeers             int  `yaml:"MaxPeers" env:"P2P_MAX_PEERS" env-default:"60" env-description:"Maximum number of connected peers"`
	DynamicMaxPeers      bool `yaml:"DynamicMaxPeers" env:"P2P_DYNAMIC_MAX_PEERS" env-default:"true" env-description:"Automatically adjust MaxPeers based on committee count"`
	DynamicMaxPeersLimit int  `yaml:"DynamicMaxPeersLimit" env:"P2P_DYNAMIC_MAX_PEERS_LIMIT" env-default:"150" env-description:"Upper limit for MaxPeers when DynamicMaxPeers is enabled"`
	TopicMaxPeers        int  `yaml:"TopicMaxPeers" env:"P2P_TOPIC_MAX_PEERS" env-default:"10" env-description:"Maximum peers per pubsub topic"`

	// Subnets is a static bit list of subnets that this node will register upon start.
	Subnets string `yaml:"Subnets" env:"SUBNETS" env-description:"Hex string (32 characters) representing 128 subnets to join on startup. Each bit corresponds to a subnet - 1 means join, 0 means skip. Examples: '0x0000000000000000000000000000ffff' (join last 16 subnets), '0xffffffffffffffffffffffffffffffff' (join all 128 subnets)"`
	// PubSubScoring is a flag to turn on/off pubsub scoring
	PubSubScoring bool `yaml:"PubSubScoring" env:"PUBSUB_SCORING" env-default:"true" env-description:"Enable pubsub peer scoring"`
	// PubSubTrace is a flag to turn on/off pubsub tracing in logs
	PubSubTrace bool `yaml:"PubSubTrace" env:"PUBSUB_TRACE" env-description:"Enable pubsub debug tracing in logs"`
	// DiscoveryTrace is a flag to turn on/off discovery tracing in logs
	DiscoveryTrace bool `yaml:"DiscoveryTrace" env:"DISCOVERY_TRACE" env-description:"Enable discovery debug tracing in logs"`
	// NetworkPrivateKey is used for network identity, MUST be injected
	NetworkPrivateKey *ecdsa.PrivateKey
	// OperatorSigner is used for signing with operator private key, MUST be injected
	OperatorSigner keys.OperatorSigner
	// OperatorPubKeyHash is hash of operator public key, used for identity, optional
	OperatorPubKeyHash string
	// OperatorDataStore contains own operator data including its ID
	OperatorDataStore operatordatastore.OperatorDataStore
	// Router propagate incoming network messages to the responsive components
	Router network.MessageRouter
	// UserAgent to use by libp2p identify protocol
	UserAgent string
	// NodeStorage is used to get operator metadata.
	NodeStorage storage.Storage
	// Network defines a network configuration.
	Network networkconfig.NetworkConfig
	// MessageValidator validates incoming messages.
	MessageValidator validation.MessageValidator

	PubsubMsgCacheTTL         time.Duration `yaml:"PubsubMsgCacheTTL" env:"PUBSUB_MSG_CACHE_TTL" env-description:"Duration to remember a message ID as seen"`
	PubsubOutQueueSize        int           `yaml:"PubsubOutQueueSize" env:"PUBSUB_OUT_Q_SIZE" env-description:"Size of the outbound pubsub message queue"`
	PubsubValidationQueueSize int           `yaml:"PubsubValidationQueueSize" env:"PUBSUB_VAL_Q_SIZE" env-description:"Size of the pubsub validation queue"`
	PubsubValidateThrottle    int           `yaml:"PubsubValidateThrottle" env:"PUBSUB_VAL_THROTTLE" env-description:"Number of goroutines for pubsub message validation"`

	// FullNode determines whether the network should sync decided history from peers.
	// If false, SyncDecidedByRange becomes a no-op.
	FullNode bool

	DisableIPRateLimit bool `yaml:"DisableIPRateLimit" env:"DISABLE_IP_RATE_LIMIT" default:"false" env-description:"Disable IP-based rate limiting"`

	GetValidatorStats network.GetValidatorStats

	// PeerScoreInspector is called periodically to inspect the peer scores.
	PeerScoreInspector func(peerMap map[peer.ID]*pubsub.PeerScoreSnapshot)

	// PeerScoreInspectorInterval is the interval at which the PeerScoreInspector is called.
	PeerScoreInspectorInterval time.Duration
}

// Libp2pOptions creates options list for the libp2p host
// these are the most basic options required to start a network instance,
// other options and libp2p components can be configured on top
func (c *Config) Libp2pOptions(logger *zap.Logger) ([]libp2p.Option, error) {
	if c.NetworkPrivateKey == nil {
		return nil, errors.New("could not create options w/o network key")
	}
	sk, err := commons.ECDSAPrivToInterface(c.NetworkPrivateKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not convert to interface priv key")
	}

	opts := []libp2p.Option{
		libp2p.Identity(sk),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.UserAgent(c.UserAgent),
	}

	opts, err = c.configureAddrs(logger, opts)
	if err != nil {
		return opts, errors.Wrap(err, "could not setup addresses")
	}

	opts = append(opts, libp2p.Security(noise.ID, noise.New))
	opts = append(opts, libp2p.Ping(true))
	opts = append(opts, libp2p.EnableNATService())
	opts = append(opts, libp2p.AutoNATServiceRateLimit(15, 3, 1*time.Minute))

	return opts, nil
}

func (c *Config) configureAddrs(logger *zap.Logger, opts []libp2p.Option) ([]libp2p.Option, error) {
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

	if c.Discovery != localDiscvery {
		maIP, err := commons.BuildMultiAddress(ipAddr.String(), "tcp", uint(c.TCPPort), "")
		if err != nil {
			return opts, errors.Wrap(err, "could not build multi address for zero address")
		}
		addrs = append(addrs, maIP)
	}
	opts = append(opts, libp2p.ListenAddrs(addrs...))

	// note, only one of (HostDNS, HostAddress) can be used with libp2p - if multiple of these
	// are set we have to prioritize between them.
	if c.HostDNS != "" {
		// AddrFactory for DNS address if provided
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			external, err := ma.NewMultiaddr(fmt.Sprintf("/dns4/%s/tcp/%d", c.HostDNS, c.TCPPort))
			if err != nil {
				logger.Error("unable to create external multiaddress", zap.Error(err))
			} else {
				addrs = append(addrs, external)
			}
			return addrs
		}))
	} else if c.HostAddress != "" {
		// AddrFactory for host address if provided
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			external, err := commons.BuildMultiAddress(c.HostAddress, "tcp", uint(c.TCPPort), "")
			if err != nil {
				logger.Error("unable to create external multiaddress", zap.Error(err))
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

	if c.Bootnodes == "" {
		return c.Network.Bootnodes
	}

	// extend additional bootnodes from config
	extraBootnodes := strings.Split(c.Bootnodes, ";")
	return append(extraBootnodes, c.Network.Bootnodes...)
}

func userAgent(fromCfg string) string {
	if len(fromCfg) > 0 {
		return fromCfg
	}
	return uc.GetBuildData()
}
