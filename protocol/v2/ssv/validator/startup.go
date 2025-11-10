package validator

import (
	"fmt"

	"github.com/ssvlabs/ssv-spec/p2p"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Start starts Validator.
func (v *Validator) Start() (started bool, err error) {
	v.mtx.Lock()
	defer v.mtx.Unlock()

	if v.stopped {
		return false, fmt.Errorf("stopped validator cannot be restarted")
	}

	if v.started {
		return false, nil // nothing to do
	}

	n, ok := v.Network.(p2p.Subscriber)
	if !ok {
		return false, fmt.Errorf("network does not support subscription")
	}
	for role := range v.DutyRunners {
		if err := n.Subscribe(v.Share.ValidatorPubKey); err != nil {
			return false, err
		}
		identifier := spectypes.NewMsgID(v.NetworkConfig.DomainType, v.Share.ValidatorPubKey[:], role)
		v.StartQueueConsumer(identifier, v.ProcessMessage)
	}

	v.started = true

	return true, nil
}

// Stop stops Validator.
func (v *Validator) Stop() {
	v.mtx.Lock()
	defer v.mtx.Unlock()

	if v.stopped || !v.started {
		return // nothing to do
	}

	v.cancel()

	v.stopped = true
}
