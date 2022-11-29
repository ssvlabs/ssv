package validator

import (
	"context"
	"github.com/bloxapp/ssv-spec/p2p"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/ssv/msgqueue"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

// Start starts a Validator.
func (v *Validator) Start() error {
	if atomic.CompareAndSwapUint32(&v.state, uint32(NotStarted), uint32(Started)) {
		n, ok := v.Network.(p2p.Subscriber)
		if !ok {
			return nil
		}
		identifiers := v.DutyRunners.Identifiers()
		for _, identifier := range identifiers {
			if err := v.loadLastHeight(identifier); err != nil {
				v.logger.Warn("could not load highest", zap.String("identifier", identifier.String()), zap.Error(err))
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
	if atomic.LoadUint32(&v.mode) == uint32(ModeR) {
		return nil
	}
	// clear the msg q
	v.Q.Clean(func(index msgqueue.Index) bool {
		return true
	})
	return nil
}

// loadLastHeight loads the highest instance from storage
func (v *Validator) loadLastHeight(identifier spectypes.MessageID) error {
	storage := v.Storage.Get(identifier.GetRoleType())
	if storage == nil {
		return errors.New("storage not found")
	}
	highestState, err := storage.GetHighestInstance(identifier[:])
	if err != nil {
		return errors.Wrap(err, "failed to get heights instance state")
	}
	if highestState == nil {
		return nil
	}
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
	if r == nil {
		return errors.New("runner is not defined")
	}
	ctrl := r.GetBaseRunner().QBFTController
	if ctrl == nil {
		return errors.New("qbft controller is not defined")
	}
	inst := instance.NewInstanceFromState(ctrl.GetConfig(), highestState)
	ctrl.Height = inst.GetHeight()
	ctrl.StoredInstances.AddNewInstance(inst)
	v.logger.Info("highest instance loaded", zap.String("role", identifier.GetRoleType().String()), zap.Int64("h", int64(inst.GetHeight())))
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
			v.logger.Debug("could not sync highest decided", zap.String("identifier", mid.String()))
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
