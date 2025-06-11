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

	started = v.started.Load()
	if started {
		return false, nil
	}

	v.startedMtx.Lock()
	defer v.startedMtx.Unlock()

	n, ok := v.Network.(p2p.Subscriber)
	if !ok {
		return false, errors.New("network does not support subscription")
	}
	for role := range v.DutyRunners {
		logger := logger.With(fields.Role(role))

		var valpk spectypes.ValidatorPK
		copy(valpk[:], v.Share.ValidatorPubKey[:])
		if err := n.Subscribe(valpk); err != nil {
			return false, err
		}

		identifier := spectypes.NewMsgID(v.NetworkConfig.GetDomainType(), v.Share.ValidatorPubKey[:], role)
		go v.StartQueueConsumer(logger, identifier, v.ProcessMessage)
	}

	v.started.Store(true)

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
