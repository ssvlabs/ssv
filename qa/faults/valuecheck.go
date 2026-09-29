package faults

// ValueCheckOverride is what a value fault asks of the faulted node's OWN value check. Every operator
// runs its checker on its own start value before it starts the QBFT instance (BaseRunner.decide), leader
// or not, so a value its honest checker refuses never reaches the wire. The override is the smallest
// weakening that lets the faulted value through; the checker a seam installs for each state is chosen at
// the seam, because the honest checker's constructor lives there.
type ValueCheckOverride int

const (
	// KeepHonestCheck leaves the honest checker in place: FAULT=none, and any value the honest checker
	// accepts (payload-root-fabricated, double-vote-index).
	KeepHonestCheck ValueCheckOverride = iota
	// DropSameSlotView keeps the honest vote checker but builds it WITHOUT the operator's own same-slot
	// view (vote-index-flip, vote-index-1-same-slot). That is exactly an honest operator whose beacon node
	// has not seen the slot's block (SIP #94 §2): it keeps the 0/1 range, slashing protection and the
	// epoch majority-fork checks, so the faulted node still refuses other out-of-range values.
	DropSameSlotView
	// PermissiveCheck swaps in PermissiveValueCheck: the value fails a check no honest operator skips
	// (vote-112b, vote-index-2, block-wrong-version, block-wrong-slot, block-wrong-proposer-index,
	// payload-root-on-external, payload-root-zero-selfbuild).
	PermissiveCheck
)

func (o ValueCheckOverride) String() string {
	switch o {
	case KeepHonestCheck:
		return "keep-honest-check"
	case DropSameSlotView:
		return "drop-same-slot-view"
	case PermissiveCheck:
		return "permissive-check"
	}
	return "unknown"
}

// PermissiveValueCheck accepts every value. A faulted operator has to start its instance with a value its
// own value check would refuse (decide checks the local input on every operator, not only the leader),
// so the faulted node — and only the faulted node — swaps its checker for this one. The side effect is
// that the faulted node also accepts invalid proposals from others; that is acceptable because every M3
// oracle is read on the honest operators, and it is recorded in qa/FAULTS.md.
//
// It satisfies protocol/v2/ssv.ValueChecker structurally, so this package needs no import of it.
type PermissiveValueCheck struct{}

func (PermissiveValueCheck) CheckValue([]byte) error { return nil }
