package network_wrapper

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	networkForkV0 "github.com/bloxapp/ssv/network/forks/v0"
	p2pv1 "github.com/bloxapp/ssv/network/p2p_v1"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter/v0"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter/v1"
	"github.com/herumi/bls-eth-go-binary/bls"
)

type P2pNetwork struct {
	ctx            context.Context
	logger         *zap.Logger
	networkAdapter adapter.Adapter
	cfgV1          *p2pv1.Config
}

func New(ctx context.Context, cfgV1 *p2pv1.Config) (network.Network, error) {
	logger := cfgV1.Logger.With(zap.String("who", "networkWrapper"))
	logger.Debug("start new wrapper")
	n := &P2pNetwork{
		ctx:    ctx,
		logger: logger,
		cfgV1:  cfgV1,
	}

	if cfgV1.Fork.IsForked() {
		logger.Debug("post fork. using v1 adapter")
		n.networkAdapter = v1.New(ctx, cfgV1, nil)
	} else {
		logger.Debug("before fork. using v0 adapter")
		cfg := *cfgV1
		cfg.Fork = networkForkV0.New() // set v0 fork
		n.networkAdapter = v0.NewV0Adapter(ctx, &cfg)
		cfgV1.Fork.SetHandler(n.onFork)
	}

	n.setup()
	n.start()
	return n, nil
}

func (p *P2pNetwork) setup() {
	if err := p.networkAdapter.Setup(); err != nil {
		p.logger.Fatal("failed to setup network adapter")
	}
}

func (p *P2pNetwork) start() {
	if err := p.networkAdapter.Start(); err != nil {
		p.logger.Fatal("failed to setup network adapter")
	}
}

func (p *P2pNetwork) onFork() {
	p.logger.Info("network fork start... moving from adapter v0 to v1")
	lis := p.networkAdapter.Listeners()
	p.logger.Info("closing current v0 adapter")
	if err := p.networkAdapter.Close(); err != nil {
		p.logger.Fatal("failed to close network adapter", zap.Error(err))
	}
	p.logger.Info("adapter v0 closed. wait for cooling...")
	// give time to the system to close all pending actions before start new network
	time.Sleep(time.Second * 3)

	// TODo  nilling previews p.networkAdapter instance?
	p.networkAdapter = v1.New(p.ctx, p.cfgV1, lis)
	p.logger.Info("setup adapter v1")
	p.setup()
	p.logger.Info("start adapter v1")
	p.start()
	// subscribe to subnets
	// start broadcast to decided topic

	p.logger.Info("fork has been completed!")
}

func (p *P2pNetwork) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	return p.networkAdapter.ReceivedMsgChan()
}

func (p P2pNetwork) ReceivedSignatureChan() (<-chan *proto.SignedMessage, func()) {
	return p.networkAdapter.ReceivedSignatureChan()
}

func (p P2pNetwork) ReceivedDecidedChan() (<-chan *proto.SignedMessage, func()) {
	return p.networkAdapter.ReceivedDecidedChan()
}

func (p P2pNetwork) ReceivedSyncMsgChan() (<-chan *network.SyncChanObj, func()) {
	return p.networkAdapter.ReceivedSyncMsgChan()
}

func (p P2pNetwork) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	return p.networkAdapter.SubscribeToValidatorNetwork(validatorPk)
}

func (p P2pNetwork) AllPeers(validatorPk []byte) ([]string, error) {
	return p.networkAdapter.AllPeers(validatorPk)
}

func (p P2pNetwork) SubscribeToMainTopic() error {
	return p.networkAdapter.SubscribeToMainTopic()
}

func (p P2pNetwork) Broadcast(topicName []byte, msg *proto.SignedMessage) error {
	return p.networkAdapter.Broadcast(topicName, msg)
}

func (p P2pNetwork) BroadcastSignature(topicName []byte, msg *proto.SignedMessage) error {
	return p.networkAdapter.BroadcastSignature(topicName, msg)
}

func (p P2pNetwork) BroadcastDecided(topicName []byte, msg *proto.SignedMessage) error {
	return p.networkAdapter.BroadcastDecided(topicName, msg)
}

func (p P2pNetwork) MaxBatch() uint64 {
	return p.networkAdapter.MaxBatch()
}

func (p P2pNetwork) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return p.networkAdapter.GetHighestDecidedInstance(peerStr, msg)
}

func (p P2pNetwork) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return p.networkAdapter.GetDecidedByRange(peerStr, msg)
}

func (p P2pNetwork) GetLastChangeRoundMsg(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return p.networkAdapter.GetLastChangeRoundMsg(peerStr, msg)
}

func (p P2pNetwork) RespondSyncMsg(streamID string, msg *network.SyncMessage) error {
	return p.networkAdapter.RespondSyncMsg(streamID, msg)
}

func (p P2pNetwork) NotifyOperatorID(oid string) {
	p.networkAdapter.NotifyOperatorID(oid)
}
