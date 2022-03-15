package v0

import (
	"context"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons/listeners"
	"github.com/bloxapp/ssv/network/forks"
	p2p_v1 "github.com/bloxapp/ssv/network/p2p_v1"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/network/p2p_v1/streams"
	"github.com/bloxapp/ssv/network/p2p_v1/topics"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/herumi/bls-eth-go-binary/bls"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	decidedTopic = "decided"

	legacyMsgStream = "/sync/0.0.1"
)

// netV0Adapter is an adapter for network v0
type netV0Adapter struct {
	ctx    context.Context
	cancel context.CancelFunc

	logger *zap.Logger

	v1Cfg *p2p_v1.Config
	fork  forks.Fork
	host  host.Host
	// TODO: use index
	knownOperators *sync.Map
	// TODO: remove after v0
	streams    *sync.Map
	streamCtrl streams.StreamController

	topicsCtrl topics.Controller
	idx        peers.Index
	disc       discovery.Service
	listeners  listeners.Container
}

// NewV0Adapter creates a new v0 network with underlying v1 infra
func NewV0Adapter(pctx context.Context, v1Cfg *p2p_v1.Config) adapter.Adapter {
	// TODO: ensure that the old user agent is passed in v1Cfg.UserAgent
	ctx, cancel := context.WithCancel(pctx)
	return &netV0Adapter{
		ctx:            ctx,
		cancel:         cancel,
		v1Cfg:          v1Cfg,
		fork:           v1Cfg.Fork, // should be v0 fork
		logger:         v1Cfg.Logger,
		listeners:      listeners.NewListenersContainer(pctx, v1Cfg.Logger),
		knownOperators: &sync.Map{},
	}
}

func (n *netV0Adapter) Listeners() listeners.Container {
	return n.listeners
}

// Setup initializes all required components
func (n *netV0Adapter) Setup() error {
	if err := n.setupHost(); err != nil {
		return errors.Wrap(err, "could not setup libp2p host")
	}
	n.streams = &sync.Map{}
	n.streamCtrl = streams.NewStreamController(n.ctx, n.logger, n.host, n.fork, n.v1Cfg.RequestTimeout)

	if err := n.setupPeerServices(); err != nil {
		return errors.Wrap(err, "could not setup peers discovery")
	}
	if err := n.setupDiscovery(); err != nil {
		return errors.Wrap(err, "could not bootstrap discovery")
	}
	if err := n.setupPubsub(); err != nil {
		return errors.Wrap(err, "could not setup pubsub")
	}

	return nil
}

// Start starts the network
func (n *netV0Adapter) Start() error {
	n.setLegacyStreamHandler()

	go func() {
		err := tasks.Retry(func() error {
			return n.disc.Bootstrap(func(e discovery.PeerEvent) {
				// TODO: check if relevant
				if err := n.host.Connect(n.ctx, e.AddrInfo); err != nil {
					n.logger.Warn("could not connect peer",
						zap.String("peer", e.AddrInfo.String()), zap.Error(err))
					return
				}
				n.logger.Debug("connected peer",
					zap.String("peer", e.AddrInfo.String()))
			})
		}, 3)
		if err != nil {
			n.logger.Panic("could not bootstrap discovery", zap.Error(err))
		}
	}()

	async.RunEvery(n.ctx, 15*time.Minute, func() {
		n.idx.GC()
	})

	async.RunEvery(n.ctx, 30*time.Second, func() {
		go n.reportAllPeers()
		n.reportTopics()
	})

	return nil
}

// Close closes the network
func (n *netV0Adapter) Close() error {
	n.cancel()
	if err := n.idx.Close(); err != nil {
		n.logger.Error("could not close index", zap.Error(err))
	}
	return n.host.Close()
}

// HandleMsg implements topics.PubsubMessageHandler
func (n *netV0Adapter) HandleMsg(topic string, msg *pubsub.Message) error {
	raw, err := n.fork.DecodeNetworkMsg(msg.Data)
	if err != nil {
		return err
	}

	cm, ok := raw.(*network.Message)
	if !ok || cm == nil || cm.SignedMessage == nil {
		n.logger.Debug("could not propagate nil message")
		return nil
	}

	n.propagateSignedMsg(cm)

	return nil
}

func (n *netV0Adapter) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_IBFTType)

	return ls.MsgChan(), n.listeners.Register(ls)
}

func (n *netV0Adapter) ReceivedSignatureChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_SignatureType)

	return ls.SigChan(), n.listeners.Register(ls)
}

func (n *netV0Adapter) ReceivedDecidedChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_DecidedType)

	return ls.DecidedChan(), n.listeners.Register(ls)
}

func (n *netV0Adapter) ReceivedSyncMsgChan() (<-chan *network.SyncChanObj, func()) {
	ls := listeners.NewListener(network.NetworkMsg_SyncType)

	return ls.SyncChan(), n.listeners.Register(ls)
}

func (n *netV0Adapter) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	topic := n.fork.ValidatorTopicID(validatorPk.Serialize())
	return n.topicsCtrl.Subscribe(topic)
}

func (n *netV0Adapter) AllPeers(validatorPk []byte) ([]string, error) {
	topic := n.fork.ValidatorTopicID(validatorPk)
	peers, err := n.topicsCtrl.Peers(topic)
	if err != nil {
		return nil, err
	}
	var results []string
	for _, p := range peers {
		pid := p.String()
		if pid == n.v1Cfg.ExporterPeerID {
			continue
		}
		results = append(results, p.String())
	}
	return results, nil
}

func (n *netV0Adapter) SubscribeToMainTopic() error {
	return n.topicsCtrl.Subscribe(decidedTopic)
}

func (n *netV0Adapter) MaxBatch() uint64 {
	return n.v1Cfg.MaxBatchResponse
}

func (n *netV0Adapter) Broadcast(validatorPK []byte, msg *proto.SignedMessage) error {
	msgBytes, err := n.fork.EncodeNetworkMsg(&network.Message{
		SignedMessage: msg,
		Type:          network.NetworkMsg_IBFTType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}
	topic := n.fork.ValidatorTopicID(validatorPK)
	if err := n.topicsCtrl.Broadcast(topic, msgBytes, time.Second*8); err != nil {
		return errors.Wrap(err, "could not broadcast signature")
	}
	return nil
}

func (n *netV0Adapter) BroadcastSignature(validatorPK []byte, msg *proto.SignedMessage) error {
	msgBytes, err := n.fork.EncodeNetworkMsg(&network.Message{
		SignedMessage: msg,
		Type:          network.NetworkMsg_SignatureType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}
	topic := n.fork.ValidatorTopicID(validatorPK)
	if err := n.topicsCtrl.Broadcast(topic, msgBytes, time.Second*8); err != nil {
		return errors.Wrap(err, "could not broadcast signature")
	}
	return nil
}

func (n *netV0Adapter) BroadcastDecided(validatorPK []byte, msg *proto.SignedMessage) error {
	msgBytes, err := n.fork.EncodeNetworkMsg(&network.Message{
		SignedMessage: msg,
		Type:          network.NetworkMsg_DecidedType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}
	topic := n.fork.ValidatorTopicID(validatorPK)
	go func() {
		if err := n.topicsCtrl.Broadcast(topic, msgBytes, time.Second*10); err != nil {
			n.logger.Error("could not broadcast message on decided topic", zap.Error(err))
		}
	}()
	if err := n.topicsCtrl.Broadcast(topic, msgBytes, time.Second*8); err != nil {
		return errors.Wrap(err, "could not broadcast decided message")
	}
	return nil
}

func (n *netV0Adapter) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return n.sendSyncRequest(peerStr, msg)
}

func (n *netV0Adapter) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return n.sendSyncRequest(peerStr, msg)
}

func (n *netV0Adapter) GetLastChangeRoundMsg(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return n.sendSyncRequest(peerStr, msg)
}

func (n *netV0Adapter) RespondSyncMsg(streamID string, msg *network.SyncMessage) error {
	msg.FromPeerID = n.host.ID().Pretty()
	resVal, ok := n.streams.Load(streamID)
	if !ok {
		return errors.New("unknown stream")
	}
	res, ok := resVal.(streams.StreamResponder)
	if !ok {
		return errors.New("coud not cast stream responder")
	}
	data, err := n.fork.EncodeNetworkMsg(&network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
		StreamID:    streamID,
	})
	if err != nil {
		return err
	}
	return res(data)
}

func (n *netV0Adapter) NotifyOperatorID(oid string) {
	n.knownOperators.Store(oid, true)
}

func (n *netV0Adapter) sendSyncRequest(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	pi, err := peer.Decode(peerStr)
	if err != nil {
		return nil, err
	}
	data, err := n.fork.EncodeNetworkMsg(&network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
	})
	if err != nil {
		return nil, err
	}
	rawRes, err := n.streamCtrl.Request(pi, legacyMsgStream, data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make sync request")
	}
	parsed, err := n.fork.DecodeNetworkMsg(rawRes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode sync request")
	}
	res, ok := parsed.(*network.Message)
	if !ok {
		return nil, errors.Wrap(err, "failed to cast sync request")
	}
	if res.SyncMessage == nil {
		return nil, errors.New("no response for sync request")
	}
	n.logger.Debug("got sync response",
		zap.String("FromPeerID", res.SyncMessage.GetFromPeerID()))
	return res.SyncMessage, nil
}

func (n *netV0Adapter) setLegacyStreamHandler() {
	n.host.SetStreamHandler("/sync/0.0.1", func(stream core.Stream) {
		req, respond, done, err := n.streamCtrl.HandleStream(stream)
		defer done()
		//cm, _, err := n.streamCtrl.HandleStream(stream)
		if err != nil {
			n.logger.Error(" highest decided preStreamHandler failed", zap.Error(err))
			return
		}
		dec, err := n.fork.DecodeNetworkMsg(req)
		if err != nil {
			n.logger.Error("could not decode message", zap.Error(err))
			return
		}
		cm, ok := dec.(*network.Message)
		if !ok {
			n.logger.Debug("bad structured message")
			return
		}
		if cm == nil {
			n.logger.Debug("got nil sync message")
			return
		}
		n.streams.Store(stream.ID(), respond)
		// adjusting message and propagating to other (internal) components
		cm.SyncMessage.FromPeerID = stream.Conn().RemotePeer().String()
		go propagateSyncMessage(n.listeners.GetListeners(network.NetworkMsg_SyncType), cm)
	})
}
