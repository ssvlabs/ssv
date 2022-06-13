package p2pv1

import (
	"context"
	"encoding/hex"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

// OnFork handles a fork event, it will close the current p2p network
// and recreate it with while preserving previous state (active validators)
// NOTE: ths method MUST be called once per fork version, otherwise we are just restarting the network
func (n *p2pNetwork) OnFork(forkVersion forksprotocol.ForkVersion) error {
	logger := n.logger.With(zap.String("where", "OnFork"))
	logger.Info("forking network")

	if forkVersion == forksprotocol.V2ForkVersion { // on fork v2 only need to change fork (soft fork)
		n.fork = forksfactory.NewFork(forkVersion)
		n.cfg.ForkVersion = forkVersion
		return nil
	}

	atomic.StoreInt32(&n.state, stateForking)
	if err := n.Close(); err != nil {
		return errors.Wrap(err, "could not close network")
	}
	atomic.StoreInt32(&n.state, stateForking)
	// waiting so for services to be closed
	logger.Info("current network instance was closed")

	<-time.After(time.Second * 6)
	ctx, cancel := context.WithCancel(n.parentCtx)
	n.ctx = ctx
	n.cancel = cancel
	n.fork = forksfactory.NewFork(forkVersion)
	n.cfg.ForkVersion = forkVersion
	if err := n.Setup(); err != nil {
		return errors.Wrap(err, "could not setup network")
	}
	if err := n.Start(); err != nil {
		return errors.Wrap(err, "could not start network")
	}
	n.resubscribeValidators()
	return nil
}

// resubscribeValidators will resubscribe to all existing validators
func (n *p2pNetwork) resubscribeValidators() {
	n.activeValidatorsLock.Lock()
	defer n.activeValidatorsLock.Unlock()

	<-time.After(time.Second * 3)
	n.logger.Debug("resubscribing validators", zap.Int("total", len(n.activeValidators)), zap.Any("values", n.activeValidators))

	success := 0
	for pk := range n.activeValidators {
		n.logger.Debug("resubscribing validator", zap.String("pk", pk))
		pkRaw, err := hex.DecodeString(pk)
		if err != nil {
			n.logger.Warn("could not decode validator public key", zap.Error(err))
			n.activeValidators[pk] = validatorStateInactive
			continue
		}
		n.activeValidators[pk] = validatorStateInactive
		if err := n.subscribe(pkRaw); err != nil {
			n.logger.Warn("could not resubscribe to validator's topic", zap.Error(err))
			// TODO: handle
			continue
		}
		n.activeValidators[pk] = validatorStateSubscribed
		success++
	}
	n.logger.Debug("resubscribed validators", zap.Int("total", len(n.activeValidators)), zap.Int("success", success))
}
