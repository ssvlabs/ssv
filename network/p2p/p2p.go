package p2p

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p"
	p2pHost "github.com/libp2p/go-libp2p-core/host"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers/scorers"
	"github.com/prysmaticlabs/prysm/shared/runutil"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
)

const (
	// DiscoveryInterval is how often we re-publish our mDNS records.
	DiscoveryInterval = time.Second

	// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
	DiscoveryServiceTag = "bloxstaking.ssv"

	// MsgChanSize is the buffer size of the message channel
	MsgChanSize = 128

	topicFmt = "bloxstaking.ssv.%s"

	syncStreamProtocol = "/sync/0.0.1"
)

type listener struct {
	msgCh     chan *proto.SignedMessage
	sigCh     chan *proto.SignedMessage
	decidedCh chan *proto.SignedMessage
	syncCh    chan *network.SyncChanObj
}

// p2pNetwork implements network.Network interface using P2P
type p2pNetwork struct {
	ctx           context.Context
	cfg           *Config
	listenersLock sync.Locker
	dv5Listener   iListener
	listeners     []listener
	logger        *zap.Logger
	privKey       *ecdsa.PrivateKey
	peers         *peers.Status
	host          p2pHost.Host
	pubsub        *pubsub.PubSub
}

// New is the constructor of p2pNetworker
func New(ctx context.Context, logger *zap.Logger, cfg *Config) (network.Network, error) {
	// init empty topics map
	cfg.Topics = make(map[string]*pubsub.Topic)

	logger = logger.With(zap.String("component", "p2p"))

	n := &p2pNetwork{
		ctx:           ctx,
		cfg:           cfg,
		listenersLock: &sync.Mutex{},
		logger:        logger,
	}

	var _ipAddr net.IP

	if cfg.DiscoveryType == "mdns" { // use mdns discovery
		// Create a new libp2p Host that listens on a random TCP port
		host, err := libp2p.New(ctx, libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
		if err != nil {
			return nil, errors.Wrap(err, "failed to create a new P2P host")
		}
		n.host = host
		n.cfg.HostID = host.ID()
	} else if cfg.DiscoveryType == "discv5" {
		dv5Nodes := n.parseBootStrapAddrs(TransformEnr(n.cfg.Enr))
		n.cfg.Discv5BootStrapAddr = dv5Nodes

		_ipAddr = n.ipAddr()
		//_ipAddr = net.ParseIP("127.0.0.1")
		logger.Info("Ip Address", zap.Any("ip", _ipAddr))

		if cfg.NetworkPrivateKey != nil {
			n.privKey = cfg.NetworkPrivateKey
		} else {
			privKey, err := privKey()
			if err != nil {
				return nil, errors.Wrap(err, "Failed to generate p2p private key")
			}
			n.privKey = privKey
		}
		opts := n.buildOptions(_ipAddr, n.privKey)
		host, err := libp2p.New(ctx, opts...)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to create p2p host")
		}
		host.RemoveStreamHandler(identify.IDDelta)
		n.host = host
	} else {
		logger.Error("Unsupported discovery flag")
		return nil, errors.New("Unsupported discovery flag")
	}

	n.logger = logger.With(zap.String("id", n.host.ID().String()))
	n.logger.Info("listening on port", zap.String("port", n.host.Addrs()[0].String()))

	// Gossipsub registration is done before we add in any new peers
	// due to libp2p's gossipsub implementation not taking into
	// account previously added peers when creating the gossipsub
	// object.
	psOpts := []pubsub.Option{
		//pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		//pubsub.WithNoAuthor(),
		//pubsub.WithMessageIdFn(msgIDFunction),
		//pubsub.WithSubscriptionFilter(s),
		pubsub.WithPeerOutboundQueueSize(256),
		pubsub.WithValidateQueueSize(256),
	}

	setPubSubParameters()

	// Create a new PubSub service using the GossipSub router
	gs, err := pubsub.NewGossipSub(ctx, n.host, psOpts...)
	if err != nil {
		n.logger.Error("Failed to start pubsub")
		return nil, err
	}
	n.pubsub = gs

	if cfg.DiscoveryType == "mdns" { // use mdns discovery {
		// Setup Local mDNS discovery
		if err := setupDiscovery(ctx, logger, n.host); err != nil {
			return nil, errors.Wrap(err, "failed to setup discovery")
		}
	} else if cfg.DiscoveryType == "discv5" {
		n.peers = peers.NewStatus(ctx, &peers.StatusConfig{
			PeerLimit: 45,
			ScorerParams: &scorers.Config{
				BadResponsesScorerConfig: &scorers.BadResponsesScorerConfig{
					Threshold:     5,
					DecayInterval: time.Hour,
				},
			},
		})

		listener, err := n.startDiscoveryV5(_ipAddr, n.privKey)
		if err != nil {
			n.logger.Error("Failed to start discovery", zap.Error(err))
			return nil, err
		}
		n.dv5Listener = listener

		err = n.connectToBootnodes()
		if err != nil {
			n.logger.Error("Could not add bootnode to the exclusion list", zap.Error(err))
			return nil, err
		}

		go n.listenForNewNodes()

		if n.cfg.HostAddress != "" {
			//logExternalIPAddr(s.host.ID(), p2pHostAddress, p2pTCPPort)
			a := net.JoinHostPort(n.cfg.HostAddress, fmt.Sprintf("%d", n.cfg.TCPPort))
			conn, err := net.DialTimeout("tcp", a, time.Second*10)
			if err != nil {
				n.logger.Error("IP address is not accessible", zap.Error(err))
			}
			if err := conn.Close(); err != nil {
				n.logger.Error("Could not close connection", zap.Error(err))
			}
		}
	}
	n.handleStream()

	runutil.RunEvery(n.ctx, 1*time.Minute, func() {
		for name, topic := range n.cfg.Topics {
			n.logger.Debug("topic peers status", zap.String("topic", name), zap.Any("peers", topic.ListPeers()))
		}
	})

	return n, nil
}

func (n *p2pNetwork) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	topic, err := n.pubsub.Join(getTopicName(validatorPk.SerializeToHexStr()))
	if err != nil {
		return errors.Wrap(err, "failed to join to Topics")
	}
	n.cfg.Topics[validatorPk.SerializeToHexStr()] = topic

	sub, err := topic.Subscribe()
	if err != nil {
		return errors.Wrap(err, "failed to subscribe on Topic")
	}

	go n.listen(sub)

	return nil
}

// IsSubscribeToValidatorNetwork checks if there is a subscription to the validator topic
func (n *p2pNetwork) IsSubscribeToValidatorNetwork(validatorPk *bls.PublicKey) bool {
	_, ok := n.cfg.Topics[validatorPk.SerializeToHexStr()]
	return ok
}

// listen listens to some validator's topic
func (n *p2pNetwork) listen(sub *pubsub.Subscription) {
	n.logger.Info("start listen to topic", zap.String("topic", sub.Topic()))
	for {
		select {
		case <-n.ctx.Done():
			t := sub.Topic()
			if err := n.cfg.Topics[t].Close(); err != nil {
				n.logger.Error("failed to close Topics", zap.Error(err))
			}
			sub.Cancel()
		default:
			msg, err := sub.Next(n.ctx)
			if err != nil {
				n.logger.Error("failed to get message from subscription Topics", zap.Error(err))
				return
			}
			var cm network.Message
			if err := json.Unmarshal(msg.Data, &cm); err != nil {
				n.logger.Error("failed to unmarshal message", zap.Error(err))
				continue
			}
			n.propagateSignedMsg(&cm)
		}
	}
}

// propagateSignedMsg takes an incoming message (from validator's topic)
// and propagates it to the corresponding internal listeners
func (n *p2pNetwork) propagateSignedMsg(cm *network.Message) {
	logger := n.logger.With(zap.String("func", "propagateSignedMsg"))
	// TODO: find a better way to deal with nil message
	// 	i.e. avoid sending nil messages in the network
	if cm == nil || cm.SignedMessage == nil {
		logger.Debug("could not propagate nil message")
		return
	}
	for _, ls := range n.listeners {
		go func(ls listener, sm *proto.SignedMessage, msgType network.NetworkMsg) {
			switch msgType {
			case network.NetworkMsg_IBFTType:
				if ls.msgCh != nil {
					ls.msgCh <- sm
				}
			case network.NetworkMsg_SignatureType:
				if ls.sigCh != nil {
					ls.sigCh <- sm
				}
			case network.NetworkMsg_DecidedType:
				if ls.decidedCh != nil {
					ls.decidedCh <- sm
				}
			default:
				logger.Error("received unsupported message", zap.Int32("msg type", int32(msgType)))
			}
		}(ls, cm.SignedMessage, cm.Type)
	}
}

// getTopic return topic by validator public key
func (n *p2pNetwork) getTopic(validatorPK []byte) (*pubsub.Topic, error) {
	if validatorPK == nil {
		return nil, errors.New("ValidatorPk is nil")
	}
	pk := &bls.PublicKey{}
	if err := pk.Deserialize(validatorPK); err != nil {
		return nil, errors.Wrap(err, "failed to deserialize publicKey")
	}
	topic := pk.SerializeToHexStr()
	if _, ok := n.cfg.Topics[topic]; !ok {
		return nil, errors.New("topic is not exist or registered")
	}
	return n.cfg.Topics[topic], nil
}

// AllPeers returns all connected peers for a validator PK (except for the validator itself)
func (n *p2pNetwork) AllPeers(validatorPk []byte) ([]string, error) {
	ret := make([]string, 0)

	topic, err := n.getTopic(validatorPk)
	if err != nil {
		return nil, err
	}

	for _, p := range topic.ListPeers() {
		ret = append(ret, peerToString(p))
	}

	return ret, nil
}

// getTopicName return formatted topic name
func getTopicName(topicName string) string {
	return fmt.Sprintf(topicFmt, topicName)
}

func (n *p2pNetwork) MaxBatch() uint64 {
	return n.cfg.MaxBatchResponse
}
