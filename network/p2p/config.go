package p2pv1

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/storage"
	uc "github.com/ssvlabs/ssv/utils/commons"
)

const minPeersBuffer = 10

// DiscoveryMode is the peer discovery a node runs, set by Config.Discovery.
type DiscoveryMode string

const (
	// DiscoveryDiscv5 finds peers over discv5, starting from the bootnodes. It is the default.
	DiscoveryDiscv5 DiscoveryMode = "discv5"
	// DiscoveryMDNS finds peers on the local network over mDNS, for a local cluster.
	DiscoveryMDNS DiscoveryMode = "mdns"
	// DiscoveryNone runs no peer discovery: the node has only its TrustedPeers, the peers that dial it, and peers
	// connected to it directly (as the test harness does). Trusted peers are dialed once, when the node starts,
	// so a trusted peer that disconnects stays disconnected unless it dials back.
	DiscoveryNone DiscoveryMode = "none"
)

// Config holds the configuration options for p2p network
type Config struct {
	Ctx          context.Context
	Bootnodes    string        `yaml:"Bootnodes" env:"BOOTNODES" env-description:"Bootnodes to use for discovery (semicolon-separated ENRs, e.g. 'enr:-abc123;enr:-def456')" `
	Discovery    DiscoveryMode `yaml:"Discovery" env:"P2P_DISCOVERY" env-description:"Discovery protocol to use (discv5, mdns, or none to run without discovery)" `
	TrustedPeers []string      `yaml:"TrustedPeers" env:"TRUSTED_PEERS" env-description:"List of peer IDs to always connect to"`

	TCPPort     uint16 `yaml:"TcpPort" env:"TCP_PORT" env-description:"TCP port for P2P transport"`
	UDPPort     uint16 `yaml:"UdpPort" env:"UDP_PORT" env-description:"UDP port for discovery"`
	HostAddress string `yaml:"HostAddress" env:"HOST_ADDRESS" env-description:"External IP address to advertise to peers (mutually exclusive with HostDNS)"`
	HostDNS     string `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS name to advertise to peers (mutually exclusive with HostAddress)"`

	RequestTimeout   time.Duration `yaml:"RequestTimeout" env:"P2P_REQUEST_TIMEOUT" env-description:"Timeout for P2P requests"`
	MaxBatchResponse uint64        `yaml:"MaxBatchResponse" env:"P2P_MAX_BATCH_RESPONSE" env-description:"Maximum number of objects returned in a batch response"`

	MaxPeers             int  `yaml:"MaxPeers" env:"P2P_MAX_PEERS" env-description:"Maximum number of connected peers"`
	DynamicMaxPeers      bool `yaml:"DynamicMaxPeers" env:"P2P_DYNAMIC_MAX_PEERS" env-description:"Automatically adjust MaxPeers based on committee count"`
	DynamicMaxPeersLimit int  `yaml:"DynamicMaxPeersLimit" env:"P2P_DYNAMIC_MAX_PEERS_LIMIT" env-description:"Upper limit for MaxPeers when DynamicMaxPeers is enabled"`
	TopicMaxPeers        int  `yaml:"TopicMaxPeers" env:"P2P_TOPIC_MAX_PEERS" env-description:"Maximum peers per pubsub topic"`

	// Subnets is a static bit list of subnets that this node will register upon start.
	Subnets string `yaml:"Subnets" env:"SUBNETS" env-description:"Hex string (32 characters) representing 128 subnets to join on startup. Each bit corresponds to a subnet - 1 means join, 0 means skip. Examples: '0x0000000000000000000000000000ffff' (join last 16 subnets), '0xffffffffffffffffffffffffffffffff' (join all 128 subnets)"`
	// PubSubScoring is a flag to turn on/off pubsub scoring
	PubSubScoring bool `yaml:"PubSubScoring" env:"PUBSUB_SCORING" env-description:"Enable pubsub peer scoring"`
	// PubSubTrace is a flag to turn on/off pubsub tracing in logs
	PubSubTrace bool `yaml:"PubSubTrace" env:"PUBSUB_TRACE" env-description:"Enable pubsub debug tracing in logs"`
	// DiscoveryTrace is a flag to turn on/off discovery tracing in logs
	DiscoveryTrace bool `yaml:"DiscoveryTrace" env:"DISCOVERY_TRACE" env-description:"Enable discovery debug tracing in logs"`
	// Libp2pTrace routes go-libp2p logs through the SSV logger at error, with the
	// swarm2 and basichost subsystems at debug (i.e. GOLOG_LOG_LEVEL=error,swarm2=debug,basichost=debug).
	// When enabled it owns go-log's level policy outright, overriding any GOLOG_LOG_LEVEL.
	Libp2pTrace bool `yaml:"Libp2pTrace" env:"LIBP2P_TRACE" env-description:"Route go-libp2p logs through the SSV logger with swarm2/basichost at debug"`
	// NetworkPrivateKey is used for network identity, MUST be injected
	NetworkPrivateKey *ecdsa.PrivateKey
	// OperatorDataStore contains own operator data including its ID
	OperatorDataStore operatordatastore.OperatorDataStore
	// Router propagate incoming network messages to the responsive components
	Router network.MessageRouter
	// UserAgent to use by libp2p identify protocol
	UserAgent string
	// NodeStorage is used to get operator metadata.
	NodeStorage storage.Storage
	// NetworkConfig defines a network configuration.
	NetworkConfig *networkconfig.Network
	// MessageValidator validates incoming messages.
	MessageValidator validation.MessageValidator

	PubsubMsgCacheTTL         time.Duration `yaml:"PubsubMsgCacheTTL" env:"PUBSUB_MSG_CACHE_TTL" env-description:"Duration to remember a message ID as seen"`
	PubsubOutQueueSize        int           `yaml:"PubsubOutQueueSize" env:"PUBSUB_OUT_Q_SIZE" env-description:"Size of the outbound pubsub message queue"`
	PubsubValidationQueueSize int           `yaml:"PubsubValidationQueueSize" env:"PUBSUB_VAL_Q_SIZE" env-description:"Size of the pubsub validation queue"`
	PubsubValidateThrottle    int           `yaml:"PubsubValidateThrottle" env:"PUBSUB_VAL_THROTTLE" env-description:"Number of goroutines for pubsub message validation"`

	// FullNode determines whether the network should sync decided history from peers.
	// If false, SyncDecidedByRange becomes a no-op.
	FullNode bool

	DisableIPRateLimit bool `yaml:"DisableIPRateLimit" env:"DISABLE_IP_RATE_LIMIT" env-description:"Disable IP-based rate limiting"`

	GetValidatorStats network.GetValidatorStats

	// PeerScoreInspector is called periodically to inspect the peer scores.
	PeerScoreInspector func(peerMap map[peer.ID]*pubsub.PeerScoreSnapshot)

	// PeerScoreInspectorInterval is the interval at which the PeerScoreInspector is called.
	PeerScoreInspectorInterval time.Duration
}

func (c *Config) ApplyDefaults() {
	c.Discovery = DiscoveryDiscv5
	c.TCPPort = 13001
	c.UDPPort = 12001
	c.RequestTimeout = 10 * time.Second
	c.MaxBatchResponse = 25
	c.MaxPeers = 60
	c.DynamicMaxPeers = true
	c.DynamicMaxPeersLimit = 150
	c.TopicMaxPeers = 10
	c.PubSubScoring = true
}

// discoveryMode returns the discovery mode to run: Discovery, or discv5 when it is unset. Read the mode only
// through it, so an unset Discovery means discv5 everywhere.
func (c *Config) discoveryMode() DiscoveryMode {
	if c.Discovery == "" {
		return DiscoveryDiscv5
	}
	return c.Discovery
}

// ValidateDiscovery rejects a Discovery that names no known mode.
func (c *Config) ValidateDiscovery() error {
	switch mode := c.discoveryMode(); mode {
	case DiscoveryDiscv5, DiscoveryMDNS, DiscoveryNone:
		return nil
	default:
		return fmt.Errorf("unknown discovery mode %q (use discv5, mdns, or none)", mode)
	}
}

// Libp2pOptions returns the basic libp2p host options; other options and components are configured on top.
// localIP is the node's local IP (nil without discv5): the host listens on it and, without HostAddress/HostDNS,
// our ENR starts with it and the host advertises it alongside its public addresses.
func (c *Config) Libp2pOptions(localIP net.IP) ([]libp2p.Option, error) {
	if c.NetworkPrivateKey == nil {
		return nil, errors.New("could not create options w/o network key")
	}
	sk, err := commons.ECDSAPrivToInterface(c.NetworkPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("could not convert to interface priv key: %w", err)
	}

	opts := make([]libp2p.Option, 0, 7)
	opts = append(opts,
		libp2p.Identity(sk),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.UserAgent(c.UserAgent),
	)

	opts, err = c.configureAddrs(opts, localIP)
	if err != nil {
		return opts, fmt.Errorf("could not setup addresses: %w", err)
	}

	opts = append(opts, libp2p.Security(noise.ID, noise.New))
	opts = append(opts, libp2p.Ping(true))
	opts = append(opts, libp2p.EnableNATService())
	opts = append(opts, libp2p.AutoNATServiceRateLimit(15, 3, 1*time.Minute))

	return opts, nil
}

// configureAddrs sets the addresses the host listens on and the ones it advertises to peers.
func (c *Config) configureAddrs(opts []libp2p.Option, localIP net.IP) ([]libp2p.Option, error) {
	addrs := make([]ma.Multiaddr, 0)
	maZero, err := commons.BuildMultiAddress("0.0.0.0", "tcp", uint(c.TCPPort), "")
	if err != nil {
		return opts, fmt.Errorf("could not build multi address for zero address: %w", err)
	}
	addrs = append(addrs, maZero)

	var maIP ma.Multiaddr
	if localIP != nil {
		// With discv5 the port goes into our ENR and the address we advertise, so the kernel can't pick it.
		if c.TCPPort == 0 {
			return opts, errors.New("missing tcp port")
		}
		maIP, err = commons.BuildMultiAddress(localIP.String(), "tcp", uint(c.TCPPort), "")
		if err != nil {
			return opts, fmt.Errorf("could not build multi address for ip address: %w", err)
		}
		addrs = append(addrs, maIP)
	}
	opts = append(opts, libp2p.ListenAddrs(addrs...))

	// announced is what we advertise besides our public addresses: HostDNS or HostAddress (mutually exclusive), else
	// localIP, the IP our ENR starts with (discv5 may later switch the ENR to the IP that peers report). It's set
	// once, at startup: unlike the public addresses, it doesn't follow a later change of the host's addresses.
	var announced ma.Multiaddr
	switch {
	case c.HostDNS != "":
		announced, err = ma.NewMultiaddr(fmt.Sprintf("/dns4/%s/tcp/%d", c.HostDNS, c.TCPPort))
	case c.HostAddress != "":
		announced, err = commons.BuildMultiAddress(c.HostAddress, "tcp", uint(c.TCPPort), "")
	case maIP != nil:
		announced = maIP
	default:
		// Without discv5 there's no ENR to name the one private address peers reach us through, so keep
		// go-libp2p's default: every interface address.
		return opts, nil
	}
	if err != nil {
		return opts, fmt.Errorf("could not build external multi address: %w", err)
	}

	// go-libp2p resolves the 0.0.0.0 listener to every interface address (docker bridges, VPNs, loopback, ...), and
	// identify and the signed peer record advertise what this factory returns. Peers reach us through the public
	// and announced addresses, so the other private ones would only leak our internal network.
	opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
		return append(publicAddrs(addrs), announced)
	}))

	return opts, nil
}

func publicAddrs(addrs []ma.Multiaddr) []ma.Multiaddr {
	public := make([]ma.Multiaddr, 0, len(addrs))
	for _, addr := range addrs {
		if manet.IsPublicAddr(addr) {
			public = append(public, addr)
		}
	}
	return public
}

// TransformBootnodes converts bootnodes string and convert it to slice
func (c *Config) TransformBootnodes() []string {
	if c.Bootnodes == "" {
		return c.NetworkConfig.Bootnodes
	}

	// extend additional bootnodes from config
	extraBootnodes := strings.Split(c.Bootnodes, ";")
	return append(extraBootnodes, c.NetworkConfig.Bootnodes...)
}

func userAgent(fromCfg string) string {
	if len(fromCfg) > 0 {
		return fromCfg
	}
	return uc.GetBuildData()
}
