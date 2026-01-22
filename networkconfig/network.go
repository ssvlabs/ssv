package networkconfig

import (
	"encoding/json"
	"fmt"
	"math"

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
const booleSubsequentWindowSlots = phase0.Slot(1)

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

func (n Network) BooleForkAtSlot(slot phase0.Slot) bool {
	return n.EstimatedEpochAtSlot(slot) >= n.SSV.Forks.Boole
}

func (n Network) InBooleTransitionWindow(slot phase0.Slot) bool {
	return n.inBoolePriorWindow(slot) || n.inBooleSubsequentWindow(slot)
}

func (n Network) inBoolePriorWindow(slot phase0.Slot) bool {
	if n.BooleForkAtSlot(slot) {
		return false
	}
	epoch := n.EstimatedEpochAtSlot(slot)
	if n.SSV.Forks.Boole == 0 {
		return false
	}
	if n.SSV.Forks.Boole <= boolePriorWindowEpochs {
		return epoch == 0
	}
	return epoch >= n.SSV.Forks.Boole-boolePriorWindowEpochs
}

func (n Network) inBooleSubsequentWindow(slot phase0.Slot) bool {
	if booleSubsequentWindowSlots == 0 {
		return false
	}
	if n.SSV.Forks.Boole == 0 {
		return false
	}
	if n.SSV.Forks.Boole == phase0.Epoch(math.MaxUint64) {
		return false
	}
	maxEpoch := phase0.Epoch(math.MaxUint64 / n.SlotsPerEpoch)
	if n.SSV.Forks.Boole > maxEpoch {
		return false
	}
	start := n.FirstSlotAtEpoch(n.SSV.Forks.Boole)
	if start > phase0.Slot(math.MaxUint64)-booleSubsequentWindowSlots {
		return false
	}
	end := start + booleSubsequentWindowSlots
	return slot >= start && slot < end
}
