package faults

import "testing"

// SetForTest activates f for the duration of the test and restores the previous value afterwards.
func SetForTest(t *testing.T, f Fault) {
	t.Helper()
	prev := Active()
	Init(f)
	t.Cleanup(func() { Init(prev) })
}
