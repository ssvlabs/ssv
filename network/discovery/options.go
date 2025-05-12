package discovery

import (
	"crypto/ecdsa"
	"net"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/network/commons"
	compatible_logger "github.com/ssvlabs/ssv/network/discovery/logger"
)

var DefaultSSVProtocolID = [6]byte{'s', 's', 'v', 'd', 'v', '5'}

// DiscV5Options for creating a new discv5 listener
type DiscV5Options struct {
	// StoragePath is the path used to store the DB (DHT)
	// if an empty path was given, the DB will be created in memory
	StoragePath string
	// IP of the node
	IP string
	// BindIP is the IP to bind to the UDP listener
	BindIP string
	// Port is the UDP port used by discv5
	Port uint16
	// TCPPort is the TCP port exposed in the ENR
	TCPPort uint16
	// NetworkKey is the private key used to create the peer.ID if the node
	NetworkKey *ecdsa.PrivateKey
	// Bootnodes is a list of bootstrapper nodes
	Bootnodes []string
	// Subnets is a bool slice represents all the subnets the node is interested in
	Subnets []byte
	// EnableLogging when true enables logs to be emitted
	EnableLogging bool
}

// DefaultOptions returns the default options
func DefaultOptions(privateKey *ecdsa.PrivateKey) DiscV5Options {
	return DiscV5Options{
		NetworkKey: privateKey,
		Bootnodes:  make([]string, 0),
		Port:       commons.DefaultUDP,
		TCPPort:    commons.DefaultTCP,
		IP:         commons.DefaultIP,
		BindIP:     net.IPv4zero.String(),
	}
}

// Validate validates the options
func (opts *DiscV5Options) Validate() error {
	if opts.NetworkKey == nil {
		return errors.New("missing private key")
	}
	if opts.Port == 0 {
		return errors.New("missing udp port")
	}
	return nil
}

// IPs returns the external ip and bind ip
func (opts *DiscV5Options) IPs() (net.IP, net.IP, string) {
	ipAddr := net.ParseIP(opts.IP)
	if ipAddr == nil {
		ipAddr = net.ParseIP(commons.DefaultIP)
	}
	n := "udp6"
	bindIP := net.ParseIP(opts.BindIP)
	if len(bindIP) == 0 {
		if ipAddr.To4() != nil {
			bindIP = net.IPv4zero
			n = "udp4"
		} else {
			bindIP = net.IPv6zero
		}
	} else if bindIP.To4() != nil {
		n = "udp4"
	}
	return ipAddr, bindIP, n
}

func WithProtocolID(protocolID [6]byte) func(config *discover.Config) {
	return func(config *discover.Config) {
		config.V5ProtocolID = &protocolID
	}
}

func WithUnhandled(unhandled chan<- discover.ReadPacket) func(config *discover.Config) {
	return func(config *discover.Config) {
		config.Unhandled = unhandled
	}
}

// DiscV5Cfg creates discv5 config from the options
func (opts *DiscV5Options) DiscV5Cfg(logger *zap.Logger, funcOpts ...func(config *discover.Config)) (*discover.Config, error) {
	dv5Cfg := &discover.Config{
		PrivateKey: opts.NetworkKey,
	}

	for _, fn := range funcOpts {
		fn(dv5Cfg)
	}

	if len(opts.Bootnodes) > 0 {
		bootnodes, err := ParseENR(nil, false, opts.Bootnodes...)
		if err != nil {
			return nil, errors.Wrap(err, "could not parse bootnodes records")
		}
		dv5Cfg.Bootnodes = bootnodes
	}

	if opts.EnableLogging {
		zapLogger := logger.Named(logging.NameDiscoveryV5Logger)
		//TODO: this is a workaround for using slog without upgrade go to 1.21
		zapHandler := compatible_logger.Option{Logger: zapLogger}.NewZapHandler()
		newLogger := log.New(zapHandler)
		dv5Cfg.Log = newLogger
	}

	return dv5Cfg, nil
}
