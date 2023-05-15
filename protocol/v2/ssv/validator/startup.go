package validator

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/bloxapp/ssv-spec/p2p"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/protocol/v2/types"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
)

// Start starts a Validator.
func (v *Validator) Start(logger *zap.Logger) error {
	logger = logger.Named(logging.NameValidator).With(fields.PubKey(v.Share.ValidatorPubKey))

	if atomic.CompareAndSwapUint32(&v.state, uint32(NotStarted), uint32(Started)) {
		n, ok := v.Network.(p2p.Subscriber)
		if !ok {
			return nil
		}
		for role, r := range v.DutyRunners {
			logger := logger.With(fields.Role(role))
			share := r.GetBaseRunner().Share
			if share == nil { // TODO: handle missing share?
				logger.Warn("❗ share is missing", fields.Role(role))
				continue
			}
			identifier := spectypes.NewMsgID(types.GetDefaultDomain(), r.GetBaseRunner().Share.ValidatorPubKey, role)
			if ctrl := r.GetBaseRunner().QBFTController; ctrl != nil {
				if err := ctrl.LoadHighestInstance(identifier[:]); err != nil {
					logger.Warn("❗failed to load highest instance",
						fields.PubKey(identifier.GetPubKey()),
						zap.Error(err))
				}
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
func (v *Validator) Stop() {
	if atomic.CompareAndSwapUint32(&v.state, uint32(Started), uint32(NotStarted)) {
		v.cancel()

		v.mtx.Lock() // write-lock for v.Queues
		defer v.mtx.Unlock()

		// clear the msg q
		v.Queues = make(map[spectypes.BeaconRole]queueContainer)
	}
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
			logger.Debug("❌ failed to sync highest decided", zap.Error(err))
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
