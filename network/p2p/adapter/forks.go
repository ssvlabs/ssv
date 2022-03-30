package adapter

import (
	"context"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

// OnFork handles a fork event, it will close the current p2p network
// and recreate it with while preserving previous state (active validators)
func (n *netV0Adapter) OnFork(forkVersion forksprotocol.ForkVersion) error {
	logger := n.logger.With(zap.String("where", "OnFork"))
	logger.Info("forking network")
	atomic.StoreInt32(&n.state, stateForking)
	if err := n.Close(); err != nil {
		return errors.Wrap(err, "could not close network adapter")
	}
	atomic.StoreInt32(&n.state, stateForking)
	// waiting so for services to be closed
	logger.Info("current network instance was closed")
	<-time.After(time.Second * 5)
	ctx, cancel := context.WithCancel(n.parentCtx)
	n.ctx = ctx
	n.cancel = cancel
	n.fork = forksfactory.NewFork(forkVersion)
	n.v1Cfg.ForkVersion = forkVersion
	if err := n.Setup(); err != nil {
		return errors.Wrap(err, "could not create network adapter")
	}
	if err := n.Start(); err != nil {
		return errors.Wrap(err, "could not start network adapter")
	}
	n.resubscribeValidators()
	return nil
}

// resubscribeValidators will resubscribe to all existing validators
func (n *netV0Adapter) resubscribeValidators() {
	n.activeValidatorsLock.Lock()
	defer n.activeValidatorsLock.Unlock()

	for pk := range n.activeValidators {
		pubkey := &bls.PublicKey{}
		if err := pubkey.DeserializeHexStr(pk); err != nil {
			n.logger.Warn("could not decode validator public key", zap.Error(err))
		}
		if err := n.SubscribeToValidatorNetwork(pubkey); err != nil {
			n.logger.Warn("could not resubscribe to validator's topic'", zap.Error(err))
			// TODO: handle
			n.activeValidators[pk] = false
		}
	}
}
