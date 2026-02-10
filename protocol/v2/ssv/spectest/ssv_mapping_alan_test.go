//go:build alan_spec

package spectest

import "testing"

func TestSSVMappingAlan(t *testing.T) {
	t.Setenv("SSV_SPEC_GOMOD", "go.spec.alan.mod")
	runSSVMappingTest(t)
}
