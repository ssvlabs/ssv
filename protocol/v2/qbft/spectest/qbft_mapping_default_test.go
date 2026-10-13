//go:build !alan_spec

package qbft

import "testing"

func TestQBFTMapping(t *testing.T) {
	runQBFTMappingTest(t)
}
