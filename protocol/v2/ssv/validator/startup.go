package validator

import (
	"fmt"

	"github.com/ssvlabs/ssv-spec/p2p"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Start starts Validator.
func (v *Validator) Start() (started bool, err error) {
	v.startedMtx.Lock()
	defer v.startedMtx.Unlock()

	if v.started {
		return false, nil
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
		go v.StartQueueConsumer(identifier, v.ProcessMessage)
	}

	v.started = true

	return true, nil
}

// Stop stops Validator.
func (v *Validator) Stop() {
	v.startedMtx.Lock()
	defer v.startedMtx.Unlock()

	if !v.started {
		return
	}

	v.cancel()

	v.started = false
}
