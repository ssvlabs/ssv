//go:build !alan_spec
// +build !alan_spec

package spectest

import "testing"

func TestSSVMapping(t *testing.T) {
	runSSVMappingTest(t)
}
