package faults

// PermissiveValueCheck accepts every value. A faulted leader has to propose a value its own value
// check would refuse, so the faulted node — and only the faulted node — swaps its checker for this
// one. The side effect is that the faulted node also accepts invalid proposals from others; that is
// acceptable because every M3 oracle is read on the honest operators, and it is recorded in
// qa/FAULTS.md.
//
// It satisfies protocol/v2/ssv.ValueChecker structurally, so this package needs no import of it.
type PermissiveValueCheck struct{}

func (PermissiveValueCheck) CheckValue([]byte) error { return nil }
