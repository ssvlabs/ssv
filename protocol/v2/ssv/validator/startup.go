package validator

import (
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/p2p"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// Start starts a Validator.
func (v *Validator) Start(logger *zap.Logger) (started bool, err error) {
	logger = logger.Named(logging.NameValidator).With(fields.PubKey(v.Share.ValidatorPubKey[:]))

	justStarted := v.started.CompareAndSwap(false, true)
	if !justStarted {
		return false, nil
	}

	n, ok := v.Network.(p2p.Subscriber)
	if !ok {
		return false, errors.New("network does not support subscription")
	}
	for role, dutyRunner := range v.DutyRunners {
		logger := logger.With(fields.Role(role))
		var share *spectypes.Share

		for _, s := range dutyRunner.GetShares() {
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
			return true, err
		}
		go v.StartQueueConsumer(logger, identifier, v.ProcessMessage)
	}
	return true, nil
}

// Stop stops a Validator.
func (v *Validator) Stop() {
	justStopped := v.started.CompareAndSwap(true, false)
	if !justStopped {
		return
	}

	v.cancel()

	v.mtx.Lock() // write-lock for v.Queues
	defer v.mtx.Unlock()

	v.Queues = make(map[spectypes.RunnerRole]queue.Queue) // clear the msg q
}
