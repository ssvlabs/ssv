package network_wrapper

import (
	"context"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/p2p"
	p2pv1 "github.com/bloxapp/ssv/network/p2p_v1"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter/v0"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter/v1"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
)

type p2pNetwork struct {
	ctx            context.Context
	logger         *zap.Logger
	networkAdapter adapter.Adapter
	fork           forks.Fork
	cfgV1          *p2pv1.Config
}

func New(ctx context.Context, logger *zap.Logger, cfgV0 *p2p.Config, cfgV1 *p2pv1.Config) (network.Network, error) {
	logger = logger.With(zap.String("who", "networkWrapper"))

	networkAdapter := v0.NewV0Adapter(ctx, cfgV1, cfgV0)
	n := &p2pNetwork{
		ctx:            ctx,
		logger:         logger,
		networkAdapter: networkAdapter,
		fork:           cfgV0.Fork,
		cfgV1:          cfgV1,
	}

	if cfgV1.Fork.IsForked() {
		n.OnFork()
	} else {
		// only need if not forked yet
		cfgV0.Fork.SetHandler(n.OnFork)
	}
	return n, nil
}

func (p *p2pNetwork) OnFork() {
	if
	//listeners := p.networkAdapter.Listeners() TODO pass to the new adapter
	if err := p.networkAdapter.Close(); err != nil {
		p.logger.Error("failed to close network adapter", zap.Error(err))
		return // TODO fatal?
	}

	// TODo  nilling previews p.networkAdapter instance?
	p.networkAdapter = v1.New(p.ctx, p.cfgV1)
	if err := p.networkAdapter.Setup(); err != nil {
		p.logger.Fatal("failed to setup network adapter")
	}
	if err := p.networkAdapter.Start(); err != nil {
		p.logger.Fatal("failed to setup network adapter")
	}
	// subscribe to subnets
	// start broadcast to decided topic
}

func (p *p2pNetwork) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	return p.networkAdapter.ReceivedMsgChan()
}

func (p p2pNetwork) ReceivedSignatureChan() (<-chan *proto.SignedMessage, func()) {
	return p.networkAdapter.ReceivedSignatureChan()
}

func (p p2pNetwork) ReceivedDecidedChan() (<-chan *proto.SignedMessage, func()) {
	return p.networkAdapter.ReceivedDecidedChan()
}

func (p p2pNetwork) ReceivedSyncMsgChan() (<-chan *network.SyncChanObj, func()) {
	return p.networkAdapter.ReceivedSyncMsgChan()
}

func (p p2pNetwork) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	return p.networkAdapter.SubscribeToValidatorNetwork(validatorPk)
}

func (p p2pNetwork) AllPeers(validatorPk []byte) ([]string, error) {
	return p.networkAdapter.AllPeers(validatorPk)
}

func (p p2pNetwork) SubscribeToMainTopic() error {
	return p.networkAdapter.SubscribeToMainTopic()
}

func (p p2pNetwork) Broadcast(topicName []byte, msg *proto.SignedMessage) error {
	return p.networkAdapter.Broadcast(topicName, msg)
}

func (p p2pNetwork) BroadcastSignature(topicName []byte, msg *proto.SignedMessage) error {
	return p.networkAdapter.BroadcastSignature(topicName, msg)
}

func (p p2pNetwork) BroadcastDecided(topicName []byte, msg *proto.SignedMessage) error {
	return p.networkAdapter.BroadcastDecided(topicName, msg)
}

func (p p2pNetwork) MaxBatch() uint64 {
	return p.networkAdapter.MaxBatch()
}

func (p p2pNetwork) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return p.networkAdapter.GetHighestDecidedInstance(peerStr, msg)
}

func (p p2pNetwork) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return p.networkAdapter.GetDecidedByRange(peerStr, msg)
}

func (p p2pNetwork) GetLastChangeRoundMsg(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return p.networkAdapter.GetLastChangeRoundMsg(peerStr, msg)
}

func (p p2pNetwork) RespondSyncMsg(streamID string, msg *network.SyncMessage) error {
	return p.networkAdapter.RespondSyncMsg(streamID, msg)
}

func (p p2pNetwork) NotifyOperatorID(oid string) {
	p.networkAdapter.NotifyOperatorID(oid)
}
