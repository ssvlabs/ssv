package validator

import (
	"sync/atomic"

	"github.com/bloxapp/ssv-spec/p2p"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
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
		logger := logger.With(fields.RunnerRole(role))
		share := dutyRunner.GetBaseRunner().Share
		if share == nil { // TODO: handle missing share?
			logger.Warn("❗ share is missing", fields.BeaconRole(role))
			continue
		}

		// TODO: rewrite this temporary workaround
		shares := dutyRunner.GetBaseRunner().Share
		firstShare := shares[maps.Keys(shares)[0]]
		senderID := firstShare.ValidatorPubKey[:]
		if len(shares) > 1 {
			committeeID := (&ssvtypes.SSVShare{Share: *firstShare}).CommitteeID()
			senderID = committeeID[:]
		}

		specRole, _ := role.Spec()
		identifier := spectypes.NewMsgID(ssvtypes.GetDefaultDomain(), senderID, specRole)
		if ctrl := dutyRunner.GetBaseRunner().QBFTController; ctrl != nil {
			highestInstance, err := ctrl.LoadHighestInstance(identifier[:])
			if err != nil {
				logger.Warn("❗failed to load highest instance",
					fields.SenderID(identifier.GetSenderID()),
					zap.Error(err))
			} else if highestInstance != nil {
				decidedValue := &spectypes.ConsensusData{}
				if err := decidedValue.Decode(highestInstance.State.DecidedValue); err != nil {
					logger.Warn("❗failed to decode decided value", zap.Error(err))
				} else {
					dutyRunner.GetBaseRunner().SetHighestDecidedSlot(decidedValue.Duty.Slot)
				}
			}
		}

		// TODO: remove compilation workaround with conversion to ValidatorPK
		if err := n.Subscribe(spectypes.ValidatorPK(identifier.GetSenderID())); err != nil {
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
		v.Queues = make(map[ssvtypes.RunnerRole]queueContainer)
	}
}
