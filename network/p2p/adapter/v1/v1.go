package v1

import (
	"context"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons/listeners"
	"github.com/bloxapp/ssv/network/forks"
	p2p "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/network/p2p/adapter"
	"github.com/bloxapp/ssv/network/p2p/discovery"
	"github.com/bloxapp/ssv/network/p2p/peers"
	"github.com/bloxapp/ssv/network/p2p/streams"
	"github.com/bloxapp/ssv/network/p2p/topics"
	"github.com/bloxapp/ssv/protocol/v1"
	core2 "github.com/bloxapp/ssv/protocol/v1/core"
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

	legacyMsgStream = "/sync/0.0.1" // TODO change to new protocol? take from protocol?
)

// netV1Adapter is an adapter for network v1
type netV1Adapter struct {
	ctx    context.Context
	cancel context.CancelFunc

	logger *zap.Logger

	v1Cfg *p2p.Config
	fork  forks.Fork
	host  host.Host
	// TODO: use index
	knownOperators *sync.Map

	// TODO: remove after v0 ( need to be removed?)
	streamsLock *sync.Mutex
	streams     map[string]streams.StreamResponder
	streamCtrl  streams.StreamController

	topicsCtrl topics.Controller
	idx        peers.Index
	disc       discovery.Service
	listeners  listeners.Container
}

// New creates a new v0 network with underlying v1 infra
func New(pctx context.Context, v1Cfg *p2p.Config, lis listeners.Container) adapter.Adapter {
	ctx, cancel := context.WithCancel(pctx)

	newLis := listeners.NewListenersContainer(pctx, v1Cfg.Logger)
	if lis != nil {
		newLis.SetListeners(lis.GetAllListeners())
	}

	return &netV1Adapter{
		ctx:            ctx,
		cancel:         cancel,
		v1Cfg:          v1Cfg,
		fork:           v1Cfg.Fork,
		logger:         v1Cfg.Logger,
		listeners:      listeners.NewListenersContainer(pctx, v1Cfg.Logger),
		knownOperators: &sync.Map{},
		streams:        map[string]streams.StreamResponder{},
		streamsLock:    &sync.Mutex{},
	}
}

func (n *netV1Adapter) Listeners() listeners.Container {
	return n.listeners
}

// Setup initializes all required components
func (n *netV1Adapter) Setup() error {
	if err := n.setupHost(); err != nil {
		return errors.Wrap(err, "could not setup libp2p host")
	}
	n.logger.Debug("started libp2p host", zap.String("peerId", n.host.ID().String()))
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
func (n *netV1Adapter) Start() error {
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
func (n *netV1Adapter) Close() error {
	n.cancel()
	if err := n.idx.Close(); err != nil {
		return errors.Wrap(err, "could not close index")
	}
	if err := n.disc.Close(); err != nil {
		return errors.Wrap(err, "could not close discovery service")
	}
	return n.host.Close()
}

// HandleMsg implements topics.PubsubMessageHandler
func (n *netV1Adapter) HandleMsg(topic string, msg *pubsub.Message) error {
	raw, err := n.fork.DecodeNetworkMsg(msg.Data)
	if err != nil {
		return err
	}

	cm, ok := raw.(*core2.SSVMessage)
	if !ok || cm == nil /*|| cm.SignedMessage == nil*/ {
		n.logger.Error("could not casting to SSVMessage")
		return nil
	}

	v0Message, err := convertToV0Message(cm)
	if err != nil {
		return errors.Wrap(err, "failed to convert v1Message to v0Message")
	}

	n.propagateSignedMsg(v0Message)
	return nil
}

func convertToV0Message(msg *core2.SSVMessage) (*network.Message, error) {
	signedMsgV0 := &proto.SignedMessage{}
	v0Msg := &network.Message{
		SignedMessage: signedMsgV0,
		SyncMessage: &network.SyncMessage{
			SignedMessages:       []*proto.SignedMessage{},
			FromPeerID:           "",
			Params:               nil,
			Lambda:               nil,
			Type:                 0,
			Error:                "",
			XXX_NoUnkeyedLiteral: struct{}{},
			XXX_unrecognized:     nil,
			XXX_sizecache:        0,
		},
	}

	switch msg.GetType() {
	case core2.SSVConsensusMsgType:
		signedMsg := &v1.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not decode consensus signed message")
		}

		var typeV0 proto.RoundState
		switch signedMsg.Message.MsgType {
		case v1.ProposalMsgType:
			typeV0 = proto.RoundState_PrePrepare
		case v1.PrepareMsgType:
			typeV0 = proto.RoundState_Prepare
		case v1.CommitMsgType:
			typeV0 = proto.RoundState_Commit
		case v1.RoundChangeMsgType:
			typeV0 = proto.RoundState_ChangeRound
			v0Msg.Type = network.NetworkMsg_IBFTType
		case v1.DecidedMsgType:
			typeV0 = proto.RoundState_Decided
			v0Msg.Type = network.NetworkMsg_DecidedType
		}

		signedMsgV0.Message = &proto.Message{
			Type:      typeV0,
			Round:     uint64(signedMsg.Message.Round),
			Lambda:    signedMsg.Message.Identifier,
			SeqNumber: uint64(signedMsg.Message.Height),
			Value:     signedMsg.Message.Data,
		}
		signedMsgV0.Signature = signedMsg.GetSignature()
		for _, signer := range signedMsg.GetSigners() {
			signedMsgV0.SignerIds = append(signedMsgV0.SignerIds, uint64(signer))
		}

		//return v.processConsensusMsg(dutyRunner, signedMsg)
	case core2.SSVPostConsensusMsgType:
		signedMsg := &v1.SignedPostConsensusMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not get post consensus Message from network Message")
		}
		//return v.processPostConsensusSig(dutyRunner, signedMsg)
	case core2.SSVSyncMsgType:
		panic("implement")
	default:
		return nil, errors.New("unknown msg")
	}

	switch msg.MsgType {
	case core2.SSVConsensusMsgType:
		v0Msg.Type = network.NetworkMsg_IBFTType
	case core2.SSVPostConsensusMsgType:
		v0Msg.Type = network.NetworkMsg_DecidedType // TODO need to provide the proper type (under consensus or post consensus?)
		v0Msg.Type = network.NetworkMsg_SignatureType
	case core2.SSVSyncMsgType:
		v0Msg.Type = network.NetworkMsg_SyncType
	}

	return v0Msg, nil
}

func (n *netV1Adapter) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_IBFTType)

	return ls.MsgChan(), n.listeners.Register(ls)
}

func (n *netV1Adapter) ReceivedSignatureChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_SignatureType)

	return ls.SigChan(), n.listeners.Register(ls)
}

func (n *netV1Adapter) ReceivedDecidedChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_DecidedType)

	return ls.DecidedChan(), n.listeners.Register(ls)
}

func (n *netV1Adapter) ReceivedSyncMsgChan() (<-chan *network.SyncChanObj, func()) {
	ls := listeners.NewListener(network.NetworkMsg_SyncType)

	return ls.SyncChan(), n.listeners.Register(ls)
}

func (n *netV1Adapter) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	topic := n.fork.ValidatorTopicID(validatorPk.Serialize())
	return n.topicsCtrl.Subscribe(topic)
}

func (n *netV1Adapter) AllPeers(validatorPk []byte) ([]string, error) {
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

func (n *netV1Adapter) SubscribeToMainTopic() error {
	return n.topicsCtrl.Subscribe(decidedTopic)
}

func (n *netV1Adapter) MaxBatch() uint64 {
	return n.v1Cfg.MaxBatchResponse
}

func (n *netV1Adapter) Broadcast(validatorPK []byte, msg *proto.SignedMessage) error {
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

func (n *netV1Adapter) BroadcastSignature(validatorPK []byte, msg *proto.SignedMessage) error {
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

func (n *netV1Adapter) BroadcastDecided(validatorPK []byte, msg *proto.SignedMessage) error {
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

func (n *netV1Adapter) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return n.sendSyncRequest(peerStr, msg)
}

func (n *netV1Adapter) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return n.sendSyncRequest(peerStr, msg)
}

func (n *netV1Adapter) GetLastChangeRoundMsg(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return n.sendSyncRequest(peerStr, msg)
}

func (n *netV1Adapter) RespondSyncMsg(streamID string, msg *network.SyncMessage) error {
	msg.FromPeerID = n.host.ID().Pretty()
	n.streamsLock.Lock()
	res, ok := n.streams[streamID]
	delete(n.streams, streamID)
	n.streamsLock.Unlock()
	if !ok {
		return errors.New("unknown stream")
	}
	data, err := n.fork.EncodeNetworkMsg(&network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
	})
	if err != nil {
		n.logger.Error("failed to encode msg", zap.Error(err))
		return err
	}
	return res(data)
}

func (n *netV1Adapter) NotifyOperatorID(oid string) {
	n.knownOperators.Store(oid, true)
}

func (n *netV1Adapter) sendSyncRequest(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
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

func (n *netV1Adapter) setLegacyStreamHandler() {
	n.host.SetStreamHandler(legacyMsgStream, func(stream core.Stream) {
		req, respond, done, err := n.streamCtrl.HandleStream(stream)
		//cm, _, err := n.streamCtrl.HandleStream(stream)
		if err != nil {
			n.logger.Error("highest decided preStreamHandler failed", zap.Error(err))
			done()
			return
		}
		dec, err := n.fork.DecodeNetworkMsg(req)
		if err != nil {
			n.logger.Error("could not decode message", zap.Error(err))
			done()
			return
		}
		cm, ok := dec.(*network.Message)
		if !ok {
			n.logger.Debug("bad structured message")
			done()
			return
		}
		if cm == nil {
			n.logger.Debug("got nil sync message")
			done()
			return
		}
		n.streamsLock.Lock()
		n.streams[stream.ID()] = func(bytes []byte) error {
			defer done()
			return respond(bytes)
		}
		n.streamsLock.Unlock()
		// adjusting message and propagating to other (internal) components
		cm.SyncMessage.FromPeerID = stream.Conn().RemotePeer().String()
		cm.StreamID = stream.ID()
		go propagateSyncMessage(n.listeners.GetListeners(network.NetworkMsg_SyncType), cm)
	})
}
