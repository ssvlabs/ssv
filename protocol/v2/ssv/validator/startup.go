package validator

import (
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/p2p"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
)

// Start starts a Validator.
func (v *Validator) Start(logger *zap.Logger) (started bool, err error) {
	logger = logger.Named(logging.NameValidator).With(fields.PubKey(v.Share.ValidatorPubKey[:]))

	if !atomic.CompareAndSwapUint32(&v.state, uint32(NotStarted), uint32(Started)) {
		return false, nil
	}

	n, ok := v.Network.(p2p.Subscriber)
	if !ok {
		return false, errors.New("network does not support subscription")
	}
	for role, dutyRunner := range v.DutyRunners {
		logger := logger.With(fields.Role(role))
		var share *spectypes.Share

		for _, s := range dutyRunner.GetBaseRunner().Share {
			if s.ValidatorPubKey == v.Share.ValidatorPubKey {
				share = s
				break
			}
		}

		if share == nil { // TODO: handle missing share?
			logger.Warn("❗ share is missing", fields.Role(role))
			continue
		}

		identifier := spectypes.NewMsgID(v.NetworkConfig.DomainType, share.ValidatorPubKey[:], role)

		// TODO: P2P
		var valpk spectypes.ValidatorPK
		copy(valpk[:], share.ValidatorPubKey[:])

		if err := n.Subscribe(valpk); err != nil {
			atomic.StoreUint32(&v.state, uint32(NotStarted))
			return false, err
		}
		go v.StartQueueConsumer(logger, identifier, v.ProcessMessage)
	}
	return true, nil
}

// Stop stops a Validator.
func (v *Validator) Stop() {
	if atomic.CompareAndSwapUint32(&v.state, uint32(Started), uint32(NotStarted)) {
		v.cancel()

		v.mtx.Lock() // write-lock for v.Queues
		defer v.mtx.Unlock()

		// clear the msg q
		v.Queues = make(map[spectypes.RunnerRole]queueContainer)
	}
}
