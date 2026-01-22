package networkconfig

import (
	"encoding/json"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type Network struct {
	*Beacon
	*SSV
}

func (n Network) String() string {
	jsonBytes, err := json.Marshal(n)
	if err != nil {
		panic(err)
	}

	return string(jsonBytes)
}

const alanForkName = "alan"
const boolePriorWindowEpochs = phase0.Epoch(1)
const booleUnsubscriptionWindowSlots = phase0.Slot(1)

// StorageName returns a config name used to make sure the stored network doesn't differ.
// It combines the network name with fork name.
func (n Network) StorageName() string {
	return fmt.Sprintf("%s:%s", n.SSV.Name, alanForkName) // TODO: decide what forks change DB fork name
}

func (n Network) GasLimit36Fork() bool {
	return n.EstimatedCurrentEpoch() >= n.SSV.Forks.GasLimit36
}

func (n Network) BooleFork() bool {
	return n.BooleForkAtSlot(n.EstimatedCurrentSlot())
}

func (n Network) BooleForkAtEpoch(epoch phase0.Epoch) bool {
	return epoch >= n.SSV.Forks.Boole
}

func (n Network) BooleForkAtSlot(slot phase0.Slot) bool {
	return n.BooleForkAtEpoch(n.EstimatedEpochAtSlot(slot))
}

func (n Network) InBooleTransitionWindow(slot phase0.Slot) bool {
	return n.inBoolePriorWindow(slot) || n.inBooleUnsubscriptionWindow(slot)
}

func (n Network) inBoolePriorWindow(slot phase0.Slot) bool {
	if n.BooleForkAtSlot(slot) {
		return false
	}
	epoch := n.EstimatedEpochAtSlot(slot)
	booleEpoch := n.SSV.Forks.Boole
	if booleEpoch <= boolePriorWindowEpochs {
		return true
	}
	return epoch >= booleEpoch-boolePriorWindowEpochs
}

func (n Network) inBooleUnsubscriptionWindow(slot phase0.Slot) bool {
	if booleUnsubscriptionWindowSlots == 0 {
		return false
	}
	start := n.FirstSlotAtEpoch(n.SSV.Forks.Boole)
	end := start + booleUnsubscriptionWindowSlots
	return slot >= start && slot < end
}
