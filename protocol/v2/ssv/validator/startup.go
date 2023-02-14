package validator

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/bloxapp/ssv-spec/p2p"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"
)

// Start starts a Validator.
func (v *Validator) Start() error {
	if atomic.CompareAndSwapUint32(&v.state, uint32(NotStarted), uint32(Started)) {
		n, ok := v.Network.(p2p.Subscriber)
		if !ok {
			return nil
		}
		for role, r := range v.DutyRunners {
			share := r.GetBaseRunner().Share
			if share == nil { // TODO: handle missing share?
				continue
			}
			identifier := spectypes.NewMsgID(r.GetBaseRunner().Share.ValidatorPubKey, role)
			if err := r.GetBaseRunner().QBFTController.LoadHighestInstance(identifier[:]); err != nil {
				v.logger.Warn("failed to load highest instance",
					zap.String("identifier", identifier.String()),
					zap.Error(err))
			}
			if err := n.Subscribe(identifier.GetPubKey()); err != nil {
				return err
			}
			go v.StartQueueConsumer(identifier, v.ProcessMessage)
			go v.sync(identifier)
		}
	}
	return nil
}

// Stop stops a Validator.
func (v *Validator) Stop() error {
	v.cancel()
	// clear the msg q
	v.Queues = make(map[spectypes.BeaconRole]queueContainer)

	return nil
}

// sync performs highest decided sync
func (v *Validator) sync(mid spectypes.MessageID) {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	// TODO: config?
	interval := time.Second
	retries := 3

	for ctx.Err() == nil {
		err := v.Network.SyncHighestDecided(mid)
		if err != nil {
			v.logger.Debug("failed to sync highest decided",
				zap.String("identifier", mid.String()),
				zap.Error(err))
			retries--
			if retries > 0 {
				interval *= 2
				time.Sleep(interval)
				continue
			}
		}
		return
	}
}
