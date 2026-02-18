//go:build alan_spec

package qbft

import "testing"

func TestQBFTMappingAlan(t *testing.T) {
	t.Setenv("SSV_SPEC_GOMOD", "go.spec.alan.mod")
	runQBFTMappingTest(t)
}
