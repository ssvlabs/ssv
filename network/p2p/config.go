package p2pv1

import (
	"context"
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
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	uc "github.com/bloxapp/ssv/utils/commons"
)

const (
	localDiscvery  = "mdns"
	minPeersBuffer = 10
)

// Config holds the configuration options for p2p network
type Config struct {
	Ctx context.Context
	// prod enr
	Bootnodes string `yaml:"Bootnodes" env:"BOOTNODES" env-description:"Bootnodes to use to start discovery, seperated with ';'" env-default:"enr:-Li4QO2k62g1tiwitaoFVMT8zN-sSNPp8cg8Kv-5lg6_6VLjVZREhxVMSmerOTptlKbBaO2iszi7rvKBYzbGf38HpcSGAYLoed50h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdWuKJc2VjcDI1NmsxoQITQ1OchoBl5XW9RfBembdN9Er1qNEOIc5ohrQ0rT9B-YN0Y3CCE4iDdWRwgg-g;enr:-Li4QAxqhjjQN2zMAAEtOF5wlcr2SFnPKINvvlwMXztJhClrfRYLrqNy2a_dMUwDPKcvM7bebq3uptRoGSV0LpYEJuyGAYRZG5n5h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQLbXMJi_Pq3imTq11EwH8MbxmXlHYvH2Drz_rsqP1rNyoN0Y3CCE4iDdWRwgg-g"`
	Discovery string `yaml:"Discovery" env:"P2P_DISCOVERY" env-description:"Discovery system to use" env-default:"discv5"`

	TCPPort     int    `yaml:"TcpPort" env:"TCP_PORT" env-default:"13001" env-description:"TCP port for p2p transport"`
	UDPPort     int    `yaml:"UdpPort" env:"UDP_PORT" env-default:"12001" env-description:"UDP port for discovery"`
	HostAddress string `yaml:"HostAddress" env:"HOST_ADDRESS" env-description:"External ip node is exposed for discovery"`
	HostDNS     string `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS node is exposed for discovery"`

	RequestTimeout   time.Duration `yaml:"RequestTimeout" env:"P2P_REQUEST_TIMEOUT"  env-default:"7s"`
	MaxBatchResponse uint64        `yaml:"MaxBatchResponse" env:"P2P_MAX_BATCH_RESPONSE" env-default:"25" env-description:"Maximum number of returned objects in a batch"`
	MaxPeers         int           `yaml:"MaxPeers" env:"P2P_MAX_PEERS" env-default:"60" env-description:"Connected peers limit for connections"`
	TopicMaxPeers    int           `yaml:"TopicMaxPeers" env:"P2P_TOPIC_MAX_PEERS" env-default:"10" env-description:"Connected peers limit per pubsub topic"`

	// Subnets is a static bit list of subnets that this node will register upon start.
	Subnets string `yaml:"Subnets" env:"SUBNETS" env-description:"Hex string that represents the subnets that this node will join upon start"`
	// PubSubScoring is a flag to turn on/off pubsub scoring
	PubSubScoring bool `yaml:"PubSubScoring" env:"PUBSUB_SCORING" env-default:"true" env-description:"Flag to turn on/off pubsub scoring"`
	// PubSubTrace is a flag to turn on/off pubsub tracing in logs
	PubSubTrace bool `yaml:"PubSubTrace" env:"PUBSUB_TRACE" env-description:"Flag to turn on/off pubsub tracing in logs"`
	// DiscoveryTrace is a flag to turn on/off discovery tracing in logs
	DiscoveryTrace bool `yaml:"DiscoveryTrace" env:"DISCOVERY_TRACE" env-description:"Flag to turn on/off discovery tracing in logs"`
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
	// NodeStorage is used to get operator metadata.
	NodeStorage storage.Storage
	// Network defines a network configuration.
	Network networkconfig.NetworkConfig

	PubsubMsgCacheTTL         time.Duration `yaml:"PubsubMsgCacheTTL" env:"PUBSUB_MSG_CACHE_TTL" env-description:"How long a message ID will be remembered as seen"`
	PubsubOutQueueSize        int           `yaml:"PubsubOutQueueSize" env:"PUBSUB_OUT_Q_SIZE" env-description:"The size that we assign to the outbound pubsub message queue"`
	PubsubValidationQueueSize int           `yaml:"PubsubValidationQueueSize" env:"PUBSUB_VAL_Q_SIZE" env-description:"The size that we assign to the pubsub validation queue"`
	PubsubValidateThrottle    int           `yaml:"PubsubPubsubValidateThrottle" env:"PUBSUB_VAL_THROTTLE" env-description:"The amount of goroutines used for pubsub msg validation"`

	// FullNode determines whether the network should sync decided history from peers.
	// If false, SyncDecidedByRange becomes a no-op.
	FullNode bool

	GetValidatorStats network.GetValidatorStats

	PermissionedActivateEpoch   uint64 `yaml:"PermissionedActivateEpoch" env:"PERMISSIONED_ACTIVE_EPOCH" env-default:"99999999999999" env-description:"On which epoch to start only accepting peers that are operators registered in the contract"`
	PermissionedDeactivateEpoch uint64 `yaml:"PermissionedDeactivateEpoch" env:"PERMISSIONED_DEACTIVE_EPOCH" env-default:"0" env-description:"On which epoch to start accepting operators all peers"`

	Permissioned func() bool // this is not loaded from config file but set up in full node setup
	// WhitelistedOperatorKeys is an array of Operator Public Key PEMs not registered in the contract with which the node will accept connections
	WhitelistedOperatorKeys []string `yaml:"WhitelistedOperatorKeys" env:"WHITELISTED_KEYS" env-description:"Operators' keys not registered in the contract with which the node will accept connections"`
}

// StageExporterPubkeys is used to whitelist the exporters  TODO: Add to mainnet/testnet configs
var StageExporterPubkeys = []string{
	"LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBd01Lck9lU1ZzMDRaSk00OFZuZGgKMmJqVmtSMFRiblZxZVBrZVA4L3RDMUVOTFhHYW5zb0cvUzRYdEQ3Y25zbE1Dekwvb0RuTFZLL2lBVndtelNJQQpUZTZOWXdMY3hQVzhiNlo2d0ZBZ0RjSm1ZeVBYRUxYMGJ1UWhOL01ZSFE0bnJvRlpwdmRhUXBGS0w3Tjk1cHhzCnlkeXArUmJjVzcyWnFjamlmR1cvVytsblpzZStmVjRlODU3a1pSY29UQUJHdGxsQ1p0N3BNeGU4blYxSmRFaEoKenFSNDdabjQ0SE4zVUZIbFFSMjBTNWxkSlRzSFdkMExJcjNlYmJWSnV0Uzd2ZmxEbThYOWhoUXErYnpjZ2JQOQplejlZTjVFdzRaRE8xcGFkVlkzOFdiVzFEZWxoUkpLTFNhRjN0RXlpZE1mNkk2bmJJekRQVWpIZ1lYSjZLQ3I5CjJ3SURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K",
	"LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBbk9aQzNieDkyUE50WU93VnpDa2IKa3djZk84VGRsaW1vYjNzTEZtUE1VTzhNai9IUEp1N2RUU1JpMWpkTDZQNVNLbVNtMzl2cXdIRURxM3dtQzFjVwpsTzlTWlg5bDNORDNmM1VBN1JIK283WUFRa0VpakVTMUE0RUVmOTdKSkdqdE1SUTExRWFpelZkUnVSamRxU1RSClVIdE80Z3ZZK0NGTnEzSDZOdXh2OFVmL2lOV3ZyQWxleDdzWFlzUHF6SUQveHR6UGJGbXduZlE1bC9kUlgwYUEKYkpLVzJraElBdmpxSitkam5PMWdkWE9zc0xQZEFHM3pySkdJQnBWenpIaERtRUVmSVFrQUd5Mi9WYVBkcHd3dQpCNTlNRGJ2TmtLakdWR1c1VGl4R2hzaWN6Mmh4b3dhSlJjaHF1V042djZrcEpPTEFUYVkySHMzL1pSTlNwRmZJCjFRSURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K",
}

// Libp2pOptions creates options list for the libp2p host
// these are the most basic options required to start a network instance,
// other options and libp2p components can be configured on top
func (c *Config) Libp2pOptions(logger *zap.Logger, fork forks.Fork) ([]libp2p.Option, error) {
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

	opts, err = c.configureAddrs(logger, opts)
	if err != nil {
		return opts, errors.Wrap(err, "could not setup addresses")
	}

	opts = append(opts, libp2p.Security(noise.ID, noise.New))

	opts = fork.AddOptions(opts)

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

	// AddrFactory for host address if provided
	if c.HostAddress != "" {
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
	// AddrFactory for DNS address if provided
	if c.HostDNS != "" {
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			external, err := ma.NewMultiaddr(fmt.Sprintf("/dns4/%s/tcp/%d", c.HostDNS, c.TCPPort))
			if err != nil {
				logger.Warn("unable to create external multiaddress", zap.Error(err))
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
		items = append(items, c.Network.Bootnodes...)
	}
	return items
}

func userAgent(fromCfg string) string {
	if len(fromCfg) > 0 {
		return fromCfg
	}
	return uc.GetBuildData()
}
