package validator

import (
	"context"
	"github.com/bloxapp/ssv-spec/p2p"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

// Start starts a Validator.
func (v *Validator) Start(logger *zap.Logger) error {
	logger.Named(logging.NameValidator)

	if atomic.CompareAndSwapUint32(&v.state, uint32(NotStarted), uint32(Started)) {
		n, ok := v.Network.(p2p.Subscriber)
		if !ok {
			return nil
		}
		for role, r := range v.DutyRunners {
			share := r.GetBaseRunner().Share
			if share == nil { // TODO: handle missing share?
				logger.Warn("❗ share is missing", zap.String("role", role.String()))
				continue
			}
			identifier := spectypes.NewMsgID(r.GetBaseRunner().Share.ValidatorPubKey, role)
			if err := r.GetBaseRunner().QBFTController.LoadHighestInstance(identifier[:]); err != nil {
				logger.Warn("❗ failed to load highest instance",
					zap.String("identifier", identifier.String()),
					zap.Error(err))
			}
			if err := n.Subscribe(identifier.GetPubKey()); err != nil {
				return err
			}
			go v.StartQueueConsumer(logger, identifier, v.ProcessMessage)
			go v.sync(logger, identifier)
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
func (v *Validator) sync(logger *zap.Logger, mid spectypes.MessageID) {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	// TODO: config?
	interval := time.Second
	retries := 3

	for ctx.Err() == nil {
		err := v.Network.SyncHighestDecided(mid)
		if err != nil {
			logger.Debug("❌ failed to sync highest decided",
				fields.MessageID(mid),
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
