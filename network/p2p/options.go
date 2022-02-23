package p2p

import (
	"fmt"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
	noise "github.com/libp2p/go-libp2p-noise"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	libp2ptcp "github.com/libp2p/go-tcp-transport"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

const (
	// overlay parameters
	gossipSubD   = 8 // topic stable mesh target count
	gossipSubDlo = 6 // topic stable mesh low watermark
	//gossipSubDhi = 12 // topic stable mesh high watermark

	// gossipMaxIHaveLength is max number fo ihave messages to send
	// lower the maximum (default is 5000) to avoid ihave floods
	gossipMaxIHaveLength = 2000

	// gossip parameters
	gossipSubMcacheLen    = 4 // number of windows to retain full messages in cache for `IWANT` responses
	gossipSubMcacheGossip = 3 // number of windows to gossip about
	//gossipSubSeenTTL      = 550 // number of heartbeat intervals to retain message IDs

	// heartbeat interval
	gossipSubHeartbeatInterval = 700 * time.Millisecond // frequency of heartbeat, milliseconds

	// pubsubQueueSize is the size that we assign to our validation queue and outbound message queue
	pubsubQueueSize = 512
)

// buildOptions for the libp2p host.
func (n *p2pNetwork) buildOptions(cfg *Config) ([]libp2p.Option, error) {
	options := []libp2p.Option{
		privKeyOption(n.privKey),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
	}

	switch cfg.DiscoveryType {
	case discoveryTypeMdns:
		options = append(options, libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
		n.logger.Debug("build network options with mdns discovery")
		return options, nil
	case discoveryTypeDiscv5:
		n.logger.Debug("build network options with discv5 discovery")
	default:
		return nil, errors.New("unsupported discovery flag")
	}

	addrOpts, err := n.configureAddrs()
	if err != nil {
		return options, err
	}
	options = append(options, addrOpts...)

	options = append(options, libp2p.UserAgent(n.getUserAgent()))

	options = append(options, libp2p.Security(noise.ID, noise.New))

	options = append(options, libp2p.EnableNATService())

	options = append(options, libp2p.ConnectionGater(n))

	//if cfg.EnableUPnP {
	//	options = append(options, libp2p.NATPortMap()) // Allow to use UPnP
	//}
	//if cfg.RelayNodeAddr != "" {
	//	options = append(options, libp2p.AddrsFactory(withRelayAddrs(cfg.RelayNodeAddr)))
	//} else {
	// Disable relay if it has not been set.
	options = append(options, libp2p.DisableRelay())
	//}
	// Disable Ping Service.
	options = append(options, libp2p.Ping(false))
	return options, nil
}

func (n *p2pNetwork) configureAddrs() ([]libp2p.Option, error) {
	var opts []libp2p.Option
	// listen on the given port and IP
	ip, err := ipAddr()
	if err != nil {
		n.logger.Fatal("could not get IPv4 address", zap.Error(err))
	}
	n.logger.Info("IP Address", zap.Any("ip", ip))
	listen, err := buildMultiAddress(ip.String(), uint(n.cfg.TCPPort))
	if err != nil {
		return opts, errors.Wrap(err, "failed to build multi address")
	}
	listenZero, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", n.cfg.TCPPort))
	if err != nil {
		return opts, errors.Wrap(err, "failed to build multi address")
	}
	opts = append(opts, libp2p.ListenAddrs(listen, listenZero))
	// AddrFactory for host address if provided
	if n.cfg.HostAddress != "" {
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			external, err := buildMultiAddress(n.cfg.HostAddress, uint(n.cfg.TCPPort))
			if err != nil {
				n.logger.Error("Unable to create external multiaddress", zap.Error(err))
			} else {
				addrs = append(addrs, external)
			}
			return addrs
		}))
	}
	// AddrFactory for DNS address if provided
	if n.cfg.HostDNS != "" {
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			external, err := ma.NewMultiaddr(fmt.Sprintf("/dns4/%s/tcp/%d", n.cfg.HostDNS, n.cfg.TCPPort))
			if err != nil {
				n.logger.Error("Unable to create external multiaddress", zap.Error(err))
			} else {
				addrs = append(addrs, external)
			}
			return addrs
		}))
	}
	return opts, nil
}

// newGossipPubsub create a new configured instance of pubsub.PubSub
func (n *p2pNetwork) newGossipPubsub(cfg *Config) (*pubsub.PubSub, error) {
	// Gossipsub registration is done before we add in any new peers
	// due to libp2p's gossipsub implementation not taking into
	// account previously added peers when creating the gossipsub
	// object.
	psOpts := []pubsub.Option{
		//pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		//pubsub.WithNoAuthor(),
		//pubsub.WithMessageIdFn(n.msgId),
		//pubsub.WithSubscriptionFilter(s),
		pubsub.WithPeerOutboundQueueSize(pubsubQueueSize),
		pubsub.WithValidateQueueSize(pubsubQueueSize),
		pubsub.WithFloodPublish(true),
		pubsub.WithGossipSubParams(pubsubGossipParam()),
	}
	if len(cfg.ExporterPeerID) > 0 {
		exporterPeerID, err := peerFromString(cfg.ExporterPeerID)
		if err != nil {
			n.logger.Error("could not parse exporter peer id", zap.Error(err))
		} else {
			psOpts = append(psOpts, pubsub.WithDirectPeers([]peer.AddrInfo{{ID: exporterPeerID}}))
		}
	}

	// TODO: change this implementation as part of networking epic
	if cfg.PubSubTraceOut == "log" {
		psOpts = append(psOpts, pubsub.WithEventTracer(newTracer(n.logger, psTraceStateWithLogging)))
	} else if len(cfg.PubSubTraceOut) > 0 {
		tracer, err := pubsub.NewPBTracer(cfg.PubSubTraceOut)
		if err != nil {
			return nil, errors.Wrap(err, "could not create pubsub tracer")
		}
		n.logger.Debug("pubusb trace file was created", zap.String("path", cfg.PubSubTraceOut))
		psOpts = append(psOpts, pubsub.WithEventTracer(tracer))
	} else {
		// if pubsub trace flag was not set, turn on pubsub tracer with prometheus reporting
		psOpts = append(psOpts, pubsub.WithEventTracer(newTracer(n.logger, psTraceStateWithReporting)))
	}

	setGlobalPubSubParameters()

	// Create a new PubSub service using the GossipSub router
	return pubsub.NewGossipSub(n.ctx, n.host, psOpts...)
}

// creates a custom gossipsub parameter set.
func pubsubGossipParam() pubsub.GossipSubParams {
	gParams := pubsub.DefaultGossipSubParams()
	gParams.Dlo = gossipSubDlo
	gParams.D = gossipSubD
	gParams.HeartbeatInterval = gossipSubHeartbeatInterval
	gParams.HistoryLength = gossipSubMcacheLen
	gParams.HistoryGossip = gossipSubMcacheGossip
	gParams.MaxIHaveLength = gossipMaxIHaveLength
	// Set a larger gossip history to ensure that slower
	// messages have a longer time to be propagated. This
	// comes with the tradeoff of larger memory usage and
	// size of the seen message cache.
	// TODO: enable after checking for optimal values
	//if features.Get().EnableLargerGossipHistory {
	//	gParams.HistoryLength = 12
	//	gParams.HistoryGossip = 5
	//}
	return gParams
}

// We have to unfortunately set this globally in order
// to configure our message id time-cache rather than instantiating
// it with a router instance.
func setGlobalPubSubParameters() {
	pubsub.TimeCacheDuration = 550 * gossipSubHeartbeatInterval
}
