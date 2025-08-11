package validator

import (
	"fmt"
	"sync/atomic"

	"github.com/ssvlabs/ssv-spec/p2p"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Start starts a Validator.
func (v *Validator) Start() (started bool, err error) {
	if !atomic.CompareAndSwapUint32(&v.state, uint32(NotStarted), uint32(Started)) {
		return false, nil
	}

	n, ok := v.Network.(p2p.Subscriber)
	if !ok {
		return false, fmt.Errorf("network does not support subscription")
	}
	for role := range v.DutyRunners {
		identifier := spectypes.NewMsgID(v.NetworkConfig.DomainType, v.Share.ValidatorPubKey[:], role)

		if err := n.Subscribe(v.Share.ValidatorPubKey); err != nil {
			atomic.StoreUint32(&v.state, uint32(NotStarted))
			return false, err
		}
		go v.StartQueueConsumer(identifier, v.ProcessMessage)
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
