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

// storageCompatToken is the suffix baked into StorageName. It is an opaque
// storage-compatibility token, not a statement of the network's current fork:
// changing it makes every existing node fail startup with a config-lock mismatch
// until the DB is removed. A change to the stored data layout is normally handled
// by a migration instead — migrations run before the config-lock check and upgrade
// data in place with this token untouched (e.g. the gob→ssz share re-encoding).
// Bump the token only as the escape hatch for when old data is unrecoverable and
// a deliberate, coordinated network-wide resync is intended. The Boole fork keeps
// the schema dual-fork-aware on the same DB, so the historical "alan" value stays.
const storageCompatToken = "alan"

const boolePriorWindowEpochs = phase0.Epoch(1) // epochs before Boole to subscribe to both topic sets

// booleSubsequentWindowLateSlots is the lateness margin kept on top of a full epoch when deciding
// how long to keep the Alan topic set subscribed after the fork. It mirrors
// message/validation.LateSlotAllowance: a message for a pre-fork slot stays valid for maxStoredSlots
// (SlotsPerEpoch + LateSlotAllowance) slots, so the last pre-fork slots' committee-duty traffic
// (decided and post-consensus messages, published on the Alan topic keyed by their pre-fork slot)
// can legitimately arrive well after the fork. The Alan topics must remain subscribed at least that
// long or those messages are dropped by the subscription filter before validation ever sees them.
const booleSubsequentWindowLateSlots = phase0.Slot(2)

// StorageName returns a config name used to make sure the stored network doesn't differ.
// It combines the network name with the storage-compatibility token.
func (n Network) StorageName() string {
	return fmt.Sprintf("%s:%s", n.SSV.Name, storageCompatToken)
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

// BooleForkScheduled reports whether the Boole fork has an activation epoch set.
// Networks without a scheduled fork pin Forks.Boole to math.MaxUint64; scheduling
// the fork ships in a release, so a node restart is guaranteed before activation.
func (n Network) BooleForkScheduled() bool {
	return n.SSV.Forks.Boole != math.MaxUint64
}

func (n Network) BooleForkAtSlot(slot phase0.Slot) bool {
	return n.BooleForkAtEpoch(n.EstimatedEpochAtSlot(slot))
}

// BooleForkImminentOrActiveAtEpoch reports whether epoch is inside the SIP-43 prior
// window of the Boole fork (boolePriorWindowEpochs before activation) or past activation.
// Use it for acceptance decisions that must flip to post-fork behavior as soon as
// post-fork traffic can legitimately appear, so the window width stays defined in one
// place.
func (n Network) BooleForkImminentOrActiveAtEpoch(epoch phase0.Epoch) bool {
	return n.BooleForkAtEpoch(epoch + boolePriorWindowEpochs)
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
	// #nosec G115 -- SlotsPerEpoch is bounded well below math.MaxInt64
	return n.inBooleSubsequentWindowWithSlots(slot, phase0.Slot(n.SlotsPerEpoch)+booleSubsequentWindowLateSlots)
}

func (n Network) inBooleSubsequentWindowWithSlots(slot phase0.Slot, windowSlots phase0.Slot) bool {
	// If Boole is at genesis there is no transition/unsubscription window to apply; without
	// this guard we would treat slot 0 as being inside the window.
	if n.SSV.Forks.Boole == 0 {
		return false
	}

	// A malformed custom config can carry SlotsPerEpoch == 0; without this guard the
	// overflow check below would panic with a divide-by-zero.
	if n.SlotsPerEpoch == 0 {
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
