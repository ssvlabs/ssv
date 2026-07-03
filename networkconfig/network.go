package networkconfig

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
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
const boolePriorWindowEpochs = phase0.Epoch(1)    // epochs before Boole to subscribe to both topic sets
const booleSubsequentWindowSlots = phase0.Slot(1) // slots after Boole to keep accepting old-topic messages

// StorageName returns a config name used to make sure the stored network doesn't differ.
// It combines the network name with fork name.
func (n Network) StorageName() string {
	return fmt.Sprintf("%s:%s", n.SSV.Name, alanForkName) // TODO: decide what forks change DB fork name
}

func (n Network) DomainTypeAtSlot(slot phase0.Slot) spectypes.DomainType {
	if n.BooleForkAtSlot(slot) {
		return n.NextDomainType
	}
	return n.DomainType
}

func (n Network) CurrentDomainType() spectypes.DomainType {
	return n.DomainTypeAtSlot(n.EstimatedCurrentSlot())
}

func (n Network) BooleFork() bool {
	return n.BooleForkAtEpoch(n.EstimatedCurrentEpoch())
}

func (n Network) BooleForkAtEpoch(epoch phase0.Epoch) bool {
	return epoch >= n.SSV.Forks.Boole
}

func (n Network) BooleForkAtSlot(slot phase0.Slot) bool {
	return n.BooleForkAtEpoch(n.EstimatedEpochAtSlot(slot))
}

// InBooleTransitionWindow checks if the slot is in the Boole transition window,
// i.e., in `PRIOR_WINDOW` or `SUBSEQUENT_WINDOW` according to https://github.com/ssvlabs/SIPs/pull/43.
func (n Network) InBooleTransitionWindow(slot phase0.Slot) bool {
	return n.inBoolePriorWindow(slot) || n.inBooleSubsequentWindow(slot)
}

func (n Network) inBoolePriorWindow(slot phase0.Slot) bool {
	return n.inBoolePriorWindowWithEpochs(slot, boolePriorWindowEpochs)
}

func (n Network) inBoolePriorWindowWithEpochs(slot phase0.Slot, windowEpochs phase0.Epoch) bool {
	priorWindowStartEpoch := phase0.Epoch(0)
	if windowEpochs <= n.SSV.Forks.Boole {
		priorWindowStartEpoch = n.SSV.Forks.Boole - windowEpochs
	}

	return n.EstimatedEpochAtSlot(slot) >= priorWindowStartEpoch && !n.BooleForkAtSlot(slot)
}

func (n Network) inBooleSubsequentWindow(slot phase0.Slot) bool {
	return n.inBooleSubsequentWindowWithSlots(slot, booleSubsequentWindowSlots)
}

func (n Network) inBooleSubsequentWindowWithSlots(slot phase0.Slot, windowSlots phase0.Slot) bool {
	// If Boole is at genesis there is no transition/unsubscription window to apply; without
	// this guard we would treat slot 0 as being inside the window.
	if n.SSV.Forks.Boole == 0 {
		return false
	}

	// Avoid FirstSlotAtEpoch overflow when Boole is beyond the representable epoch range;
	// without this guard the multiplication would wrap and could treat small slots as in-window.
	maxEpoch := phase0.Epoch(math.MaxUint64 / n.SlotsPerEpoch)
	if n.SSV.Forks.Boole > maxEpoch {
		return false
	}

	start := n.FirstSlotAtEpoch(n.SSV.Forks.Boole)
	end := start + windowSlots
	return slot >= start && slot < end
}
