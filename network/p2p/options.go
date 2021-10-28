package p2p

import (
	"crypto/sha256"
	"fmt"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/libp2p/go-libp2p"
	noise "github.com/libp2p/go-libp2p-noise"
	yamux "github.com/libp2p/go-libp2p-yamux"
	libp2ptcp "github.com/libp2p/go-tcp-transport"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// buildOptions for the libp2p host.
func (n *p2pNetwork) buildOptions(cfg *Config) ([]libp2p.Option, error) {
	options := []libp2p.Option{
		privKeyOption(n.privKey),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
	}
	if cfg.DiscoveryType == "mdns" {
		// Create a new libp2p Host that listens on a random TCP port
		options = append(options, libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
	} else if cfg.DiscoveryType == "discv5" {
		ip := n.ipAddr()
		n.logger.Info("IP Address", zap.Any("ip", ip))
		listen, err := multiAddressBuilder(ip.String(), uint(n.cfg.TCPPort))
		if err != nil {
			return options, errors.Wrap(err, "failed to build multi address")
		}
		listenZero, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", n.cfg.TCPPort))
		if err != nil {
			return options, errors.Wrap(err, "failed to build multi address")
		}
		options = append(options, libp2p.ListenAddrs(listen, listenZero))
		ua := n.getUserAgent()
		n.logger.Info("Libp2p User Agent", zap.String("value", ua))
		options = append(options, libp2p.UserAgent(ua))

		options = append(options, libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport))

		options = append(options, libp2p.Security(noise.ID, noise.New))

		options = append(options, libp2p.EnableNATService())

		//if cfg.EnableUPnP {
		//	options = append(options, libp2p.NATPortMap()) // Allow to use UPnP
		//}
		//if cfg.RelayNodeAddr != "" {
		//	options = append(options, libp2p.AddrsFactory(withRelayAddrs(cfg.RelayNodeAddr)))
		//} else {
		// Disable relay if it has not been set.
		options = append(options, libp2p.DisableRelay())
		//}
		if cfg.HostAddress != "" {
			options = append(options, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
				external, err := multiAddressBuilder(cfg.HostAddress, uint(cfg.TCPPort))
				if err != nil {
					n.logger.Error("Unable to create external multiaddress", zap.Error(err))
				} else {
					addrs = append(addrs, external)
				}
				return addrs
			}))
		}
		if cfg.HostDNS != "" {
			options = append(options, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
				external, err := ma.NewMultiaddr(fmt.Sprintf("/dns4/%s/tcp/%d", cfg.HostDNS, cfg.TCPPort))
				if err != nil {
					n.logger.Error("Unable to create external multiaddress", zap.Error(err))
				} else {
					addrs = append(addrs, external)
				}
				return addrs
			}))
		}
		// Disable Ping Service.
		options = append(options, libp2p.Ping(false))
	} else {
		return nil, errors.New("Unsupported discovery flag")
	}
	return options, nil
}

func (n *p2pNetwork) getUserAgent() string {
	ua := commons.GetBuildData()
	if n.operatorPrivKey != nil {
		operatorPubKey, err := rsaencryption.ExtractPublicKey(n.operatorPrivKey)
		if err != nil || len(operatorPubKey) == 0 {
			n.logger.Error("could not extract operator public key", zap.Error(err))
		}
		h := sha256.Sum256([]byte(operatorPubKey))
		ua = fmt.Sprintf("%s:%x", ua, h)
	}
	return ua
}
