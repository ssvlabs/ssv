package validator

import (
	"sync/atomic"

	"github.com/pkg/errors"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv-spec/p2p"
	"github.com/ssvlabs/ssv/logging"

	"go.uber.org/zap"

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
		logger := logger.With(fields.GenesisRole(role))
		share := dutyRunner.GetBaseRunner().Share
		if share == nil { // TODO: handle missing share?
			logger.Warn("❗ share is missing", fields.GenesisRole(role))
			continue
		}
		identifier := genesisspectypes.NewMsgID(genesisspectypes.DomainType(dutyRunner.GetBaseRunner().Share.DomainType), dutyRunner.GetBaseRunner().Share.ValidatorPubKey[:], role)
		if ctrl := dutyRunner.GetBaseRunner().QBFTController; ctrl != nil {
			highestInstance, err := ctrl.LoadHighestInstance(identifier[:])
			if err != nil {
				logger.Warn("❗failed to load highest instance",
					fields.PubKey(identifier.GetPubKey()),
					zap.Error(err))
			} else if highestInstance != nil {
				decidedValue := &genesisspectypes.ConsensusData{}
				if err := decidedValue.Decode(highestInstance.State.DecidedValue); err != nil {
					logger.Warn("❗failed to decode decided value", zap.Error(err))
				} else {
					dutyRunner.GetBaseRunner().SetHighestDecidedSlot(decidedValue.Duty.Slot)
				}
			}
		}

		if err := n.Subscribe(dutyRunner.GetBaseRunner().Share.ValidatorPubKey); err != nil {
			return true, err
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
		v.Queues = make(map[genesisspectypes.BeaconRole]queueContainer)
	}
}
