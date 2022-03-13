package v0

import (
	"context"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons/listeners"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/p2p"
	streams_v0 "github.com/bloxapp/ssv/network/p2p/streams"
	p2p_v1 "github.com/bloxapp/ssv/network/p2p_v1"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/topics"
	"github.com/herumi/bls-eth-go-binary/bls"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

const (
	decidedTopic = "decided"

	legacyMsgStream = "/sync/0.0.1"
)

// NetV0Adapter is an adapter for network v0
type NetV0Adapter struct {
	ctx          context.Context
	cancel       context.CancelFunc
	v1           network.V1
	v1Cfg        *p2p_v1.Config
	v0Cfg        *p2p.Config
	fork         forks.Fork
	host         host.Host
	streamCtrlv0 streams_v0.StreamController
	topicsCtrl   topics.Controller
	// TODO: add discovery service
	disc discovery.Service

	listeners listeners.Container
}

// NewV0Adapter creates a new v0 network with underlying v1 infra
func NewV0Adapter(pctx context.Context, v1Cfg *p2p_v1.Config) adapter.Adapter {
	// TODO: ensure that the old user agent is passed in v1Cfg.UserAgent
	ctx, cancel := context.WithCancel(pctx)
	return &NetV0Adapter{
		ctx:    ctx,
		cancel: cancel,
		v1:     p2p_v1.New(pctx, v1Cfg),
		fork:   v1Cfg.Fork,
	}
}

// Setup initializes all required components
func (n *NetV0Adapter) Setup() error {
	n.setLegacyStreamHandler()

	// TODO: complete

	if err := n.setupDiscovery(); err != nil {
		return errors.Wrap(err, "could not bootstrap discovery")
	}
	if err := n.setupPubsub(); err != nil {
		return errors.Wrap(err, "could not setup pubsub")
	}

	return nil
}

// Start starts the network
func (n *NetV0Adapter) Start() error {
	// TODO: complete

	return nil
}

func (n *NetV0Adapter) Close() error {
	//TODO implement me
	panic("implement me")
}

// Fork is triggered to initiate a fork
func (n *NetV0Adapter) Fork() {
	// cancel current context
	n.cancel()
	// wait 3 seconds
	time.After(time.Second * 3)
	n.fork = n.v1Cfg.Fork
	if err := n.v1.Setup(); err != nil {
		n.v1Cfg.Logger.Panic("could not setup network v1", zap.Error(err))
	}
	if err := n.v1.Start(); err != nil {
		n.v1Cfg.Logger.Panic("could not start network v1", zap.Error(err))
	}
}

// HandleMsg implements topics.PubsubMessageHandler
func (n *NetV0Adapter) HandleMsg(topic string, msg *pubsub.Message) error {
	cm, err := n.fork.DecodeNetworkMsg(msg.Data)
	if err != nil {
		return err
	}

	if cm == nil || cm.SignedMessage == nil {
		n.v1Cfg.Logger.Debug("could not propagate nil message")
		return nil
	}

	n.propagateSignedMsg(cm)

	return nil
}

func (n *NetV0Adapter) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_IBFTType)

	return ls.MsgChan(), n.listeners.Register(ls)
}

func (n *NetV0Adapter) ReceivedSignatureChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_SignatureType)

	return ls.SigChan(), n.listeners.Register(ls)
}

func (n *NetV0Adapter) ReceivedDecidedChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_DecidedType)

	return ls.DecidedChan(), n.listeners.Register(ls)
}

func (n *NetV0Adapter) ReceivedSyncMsgChan() (<-chan *network.SyncChanObj, func()) {
	ls := listeners.NewListener(network.NetworkMsg_SyncType)

	return ls.SyncChan(), n.listeners.Register(ls)
}

func (n *NetV0Adapter) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	topic := n.v1Cfg.Fork.ValidatorTopicID(validatorPk.Serialize())
	return n.topicsCtrl.Subscribe(topic)
}

func (n *NetV0Adapter) AllPeers(validatorPk []byte) ([]string, error) {
	topic := n.v1Cfg.Fork.ValidatorTopicID(validatorPk)
	peers, err := n.topicsCtrl.Peers(topic)
	if err != nil {
		return nil, err
	}
	var results []string
	for _, p := range peers {
		results = append(results, p.String())
	}
	return results, nil
}

func (n *NetV0Adapter) SubscribeToMainTopic() error {
	return n.topicsCtrl.Subscribe(decidedTopic)
}

func (n *NetV0Adapter) MaxBatch() uint64 {
	return n.v0Cfg.MaxBatchResponse
}

func (n *NetV0Adapter) Broadcast(validatorPK []byte, msg *proto.SignedMessage) error {
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

func (n *NetV0Adapter) BroadcastSignature(validatorPK []byte, msg *proto.SignedMessage) error {
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

func (n *NetV0Adapter) BroadcastDecided(validatorPK []byte, msg *proto.SignedMessage) error {
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
			n.v1Cfg.Logger.Error("could not broadcast message on decided topic", zap.Error(err))
		}
	}()
	if err := n.topicsCtrl.Broadcast(topic, msgBytes, time.Second*8); err != nil {
		return errors.Wrap(err, "could not broadcast decided message")
	}
	return nil
}

func (n *NetV0Adapter) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return n.sendSyncRequest(peerStr, msg)
}

func (n *NetV0Adapter) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return n.sendSyncRequest(peerStr, msg)
}

func (n *NetV0Adapter) GetLastChangeRoundMsg(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return n.sendSyncRequest(peerStr, msg)
}

func (n *NetV0Adapter) RespondSyncMsg(streamID string, msg *network.SyncMessage) error {
	msg.FromPeerID = n.host.ID().Pretty()
	return n.streamCtrlv0.Respond(&network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
		StreamID:    streamID,
	})
}

func (n *NetV0Adapter) NotifyOperatorID(oid string) {
	// TODO
	panic("implement me")
}

func (n *NetV0Adapter) sendSyncRequest(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	pi, err := peer.Decode(peerStr)
	if err != nil {
		return nil, err
	}
	res, err := n.streamCtrlv0.Request(pi, legacyMsgStream, &network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to make sync request")
	}
	if res.SyncMessage == nil {
		return nil, errors.New("no response for sync request")
	}
	n.v1Cfg.Logger.Debug("got sync response",
		zap.String("FromPeerID", res.SyncMessage.GetFromPeerID()))
	return res.SyncMessage, nil
}

func (n *NetV0Adapter) setLegacyStreamHandler() {
	n.host.SetStreamHandler("/sync/0.0.1", func(stream core.Stream) {
		cm, _, err := n.streamCtrlv0.HandleStream(stream)
		if err != nil {
			n.v1Cfg.Logger.Error(" highest decided preStreamHandler failed", zap.Error(err))
			return
		}
		if cm == nil {
			n.v1Cfg.Logger.Debug("got nil sync message")
			return
		}
		// adjusting message and propagating to other (internal) components
		cm.SyncMessage.FromPeerID = stream.Conn().RemotePeer().String()
		go propagateSyncMessage(n.listeners.GetListeners(network.NetworkMsg_SyncType), cm)
	})
}
