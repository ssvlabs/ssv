package p2pv1

import (
	"crypto/ecdsa"
	"fmt"
	"strings"
	"time"

	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	uc "github.com/bloxapp/ssv/utils/commons"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	noise "github.com/libp2p/go-libp2p-noise"
	libp2ptcp "github.com/libp2p/go-tcp-transport"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Config holds the configuration options for p2p network
type Config struct {
	// TODO: ENR for compatibility
	// Bootnodes string `yaml:"Bootnodes" env:"BOOTNODES" env-description:"Bootnodes to use to start discovery, seperated with ';'" env-default:"enr:-LK4QMmL9hLJ1csDN4rQoSjlJGE2SvsXOETfcLH8uAVrxlHaELF0u3NeKCTY2eO_X1zy5eEKcHruyaAsGNiyyG4QWUQBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g"`
	// stage enr
	//Bootnodes string `yaml:"Bootnodes" env:"BOOTNODES" env-description:"Bootnodes to use to start discovery, seperated with ';'" env-default:"enr:-LK4QDAmZK-69qRU5q-cxW6BqLwIlWoYH-BoRlX2N7D9rXBlM7OJ9tWRRtryqvCW04geHC_ab8QmWT9QULnT0Tc5S1cBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g"`
	Bootnodes string `yaml:"Bootnodes" env:"BOOTNODES" env-description:"Bootnodes to use to start discovery, seperated with ';'" env-default:""`

	TCPPort     int    `yaml:"TcpPort" env:"TCP_PORT" env-default:"13001" env-description:"TCP port for p2p transport"`
	UDPPort     int    `yaml:"UdpPort" env:"UDP_PORT" env-default:"12001" env-description:"UDP port for discovery"`
	HostAddress string `yaml:"HostAddress" env:"HOST_ADDRESS" env-description:"External ip node is exposed for discovery"`
	HostDNS     string `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS node is exposed for discovery"`

	RequestTimeout   time.Duration `yaml:"RequestTimeout" env:"P2P_REQUEST_TIMEOUT"  env-default:"5s"`
	MaxBatchResponse uint64        `yaml:"MaxBatchResponse" env:"P2P_MAX_BATCH_RESPONSE" env-default:"50" env-description:"Maximum number of returned objects in a batch"`
	MaxPeers         int           `yaml:"MaxPeers" env:"P2P_MAX_PEERS" env-default:"250" env-description:"Connected peers limit for outbound connections, inbound connections can grow up to 2 times of this value"`

	PubSubScoring bool `yaml:"PubSubScoring" env:"PUBSUB_SCORING" env-description:"Flag to turn on/off pubsub scoring"`

	PubSubTrace bool `yaml:"PubSubTrace" env:"PUBSUB_TRACE" env-description:"Flag to turn on/off pubsub tracing in logs"`

	// TODO: remove with v0
	ExporterPeerID string `yaml:"ExporterPeerID" env:"EXPORTER_PEER_ID"  env-default:"16Uiu2HAkvaBh2xjstjs1koEx3jpBn5Hsnz7Bv8pE4SuwFySkiAuf"  env-description:"peer id of exporter"`

	// NetworkPrivateKey is used for network identity, MUST be injected
	NetworkPrivateKey *ecdsa.PrivateKey
	// OperatorPublicKey is used for operator identity, optional
	OperatorID string

	Logger    *zap.Logger
	Router    network.MessageRouter
	UserAgent string

	ForkVersion forksprotocol.ForkVersion
}

// Libp2pOptions creates options list for the libp2p host
// these are the most basic options required to start a network instance,
// other options and libp2p components can be configured on top
func (c *Config) Libp2pOptions() ([]libp2p.Option, error) {
	if c.NetworkPrivateKey == nil {
		return nil, errors.New("could not create options w/o network key")
	}
	sk := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(c.NetworkPrivateKey))

	opts := []libp2p.Option{
		libp2p.Identity(sk),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.UserAgent(c.UserAgent),
	}

	opts, err := c.configureAddrs(opts)
	if err != nil {
		return opts, errors.Wrap(err, "failed to setup addresses")
	}

	opts = append(opts, libp2p.Security(noise.ID, noise.New))

	if len(c.Bootnodes) > 0 { // not a local node
		// TODO: check
		//opts = append(opts, libp2p.EnableNATService())
		//opts = append(opts, libp2p.AutoNATServiceRateLimit(15, 3, 1*time.Minute))
		opts = append(opts, libp2p.DisableRelay())
		opts = append(opts, libp2p.Ping(false))
	}

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
				c.Logger.Error("unable to create external multiaddress", zap.Error(err))
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
