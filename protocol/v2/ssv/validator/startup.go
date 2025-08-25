package validator

import (
	"fmt"

	"github.com/ssvlabs/ssv-spec/p2p"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// Start starts Validator.
func (v *Validator) Start() (started bool, err error) {
	started = v.started.Load()
	if started {
		return false, nil
	}

	v.startedMtx.Lock()
	defer v.startedMtx.Unlock()

	n, ok := v.Network.(p2p.Subscriber)
	if !ok {
		return false, fmt.Errorf("network does not support subscription")
	}
	for role := range v.DutyRunners {
		if err := n.Subscribe(v.Share.ValidatorPubKey); err != nil {
			return false, err
		}
		identifier := spectypes.NewMsgID(v.NetworkConfig.DomainType, v.Share.ValidatorPubKey[:], role)
		go v.StartQueueConsumer(identifier, v.ProcessMessage)
	}

	v.started.Store(true)

	return true, nil
}

// Stop stops Validator.
func (v *Validator) Stop() {
	justStopped := v.started.CompareAndSwap(true, false)
	if !justStopped {
		return
	}

	v.cancel()

	// Clear the message queues, write-lock for v.Queues
	v.mtx.Lock()
	v.Queues = make(map[spectypes.RunnerRole]queue.Queue)
	v.mtx.Unlock()
}
