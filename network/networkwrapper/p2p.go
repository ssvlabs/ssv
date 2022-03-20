package networkwrapper

import (
	"context"
	"github.com/bloxapp/ssv/operator/forks"
	"time"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	p2pv1 "github.com/bloxapp/ssv/network/p2p_v1"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter/v0"
	"github.com/bloxapp/ssv/network/p2p_v1/adapter/v1"
	"github.com/herumi/bls-eth-go-binary/bls"
)

// P2pNetwork is network wrapper
type P2pNetwork struct {
	ctx            context.Context
	logger         *zap.Logger
	networkAdapter adapter.Adapter
	cfgV1          *p2pv1.Config
}

// New return p2pNetwork struct of network interface
func New(ctx context.Context, cfgV1 *p2pv1.Config, forker *forks.Forker) (network.Network, error) {
	logger := cfgV1.Logger.With(zap.String("who", "networkwrapper"))

	cfgV1.Fork = forker.GetCurrentFork().NetworkFork()

	n := &P2pNetwork{
		ctx:    ctx,
		logger: logger,
		cfgV1:  cfgV1,
	}

	if forker.IsForked() {
		logger.Debug("post fork. using v1 adapter")
		n.networkAdapter = v1.New(ctx, cfgV1, nil)
	} else {
		logger.Debug("before fork. using v0 adapter")
		n.networkAdapter = v0.NewV0Adapter(ctx, cfgV1)
		forker.AddHandler(n.onFork)
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

func (p *P2pNetwork) onFork(slot uint64) {
	p.logger.Info("network fork start... moving from adapter v0 to v1", zap.Uint64("fork slot", slot))
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

// ReceivedMsgChan is a channel that forwards new propagated messages to a subscriber
func (p *P2pNetwork) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	return p.networkAdapter.ReceivedMsgChan()
}

// ReceivedSignatureChan returns the channel with signatures
func (p P2pNetwork) ReceivedSignatureChan() (<-chan *proto.SignedMessage, func()) {
	return p.networkAdapter.ReceivedSignatureChan()
}

// ReceivedDecidedChan returns the channel for decided messages
func (p P2pNetwork) ReceivedDecidedChan() (<-chan *proto.SignedMessage, func()) {
	return p.networkAdapter.ReceivedDecidedChan()
}

// ReceivedSyncMsgChan returns the channel for sync messages
func (p P2pNetwork) ReceivedSyncMsgChan() (<-chan *network.SyncChanObj, func()) {
	return p.networkAdapter.ReceivedSyncMsgChan()
}

// SubscribeToValidatorNetwork subscribes and listens to validator's network
func (p P2pNetwork) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	return p.networkAdapter.SubscribeToValidatorNetwork(validatorPk)
}

// AllPeers returns all connected peers for a validator PK
func (p P2pNetwork) AllPeers(validatorPk []byte) ([]string, error) {
	return p.networkAdapter.AllPeers(validatorPk)
}

// SubscribeToMainTopic subscribes to main topic
func (p P2pNetwork) SubscribeToMainTopic() error {
	return p.networkAdapter.SubscribeToMainTopic()
}

// Broadcast propagates a signed message to all peers
func (p P2pNetwork) Broadcast(topicName []byte, msg *proto.SignedMessage) error {
	return p.networkAdapter.Broadcast(topicName, msg)
}

// BroadcastSignature broadcasts the given signature for the given lambda
func (p P2pNetwork) BroadcastSignature(topicName []byte, msg *proto.SignedMessage) error {
	return p.networkAdapter.BroadcastSignature(topicName, msg)
}

// BroadcastDecided broadcasts a decided instance with collected signatures
func (p P2pNetwork) BroadcastDecided(topicName []byte, msg *proto.SignedMessage) error {
	return p.networkAdapter.BroadcastDecided(topicName, msg)
}

// MaxBatch returns the maximum batch size for network responses
func (p P2pNetwork) MaxBatch() uint64 {
	return p.networkAdapter.MaxBatch()
}

// GetHighestDecidedInstance sends a highest decided request to peers and returns answers.
// If peer list is nil, broadcasts to all.
func (p P2pNetwork) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return p.networkAdapter.GetHighestDecidedInstance(peerStr, msg)
}

// GetDecidedByRange returns a list of decided signed messages up to 25 in a batch.
func (p P2pNetwork) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return p.networkAdapter.GetDecidedByRange(peerStr, msg)
}

// GetLastChangeRoundMsg returns the latest change round msg for a running instance, could return nil
func (p P2pNetwork) GetLastChangeRoundMsg(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return p.networkAdapter.GetLastChangeRoundMsg(peerStr, msg)
}

// RespondSyncMsg responds to the stream with the given message
func (p P2pNetwork) RespondSyncMsg(streamID string, msg *network.SyncMessage) error {
	return p.networkAdapter.RespondSyncMsg(streamID, msg)
}

// NotifyOperatorID updates the network regarding new operators joining the network
func (p P2pNetwork) NotifyOperatorID(oid string) {
	p.networkAdapter.NotifyOperatorID(oid)
}
