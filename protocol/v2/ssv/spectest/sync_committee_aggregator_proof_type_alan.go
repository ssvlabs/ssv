//go:build alan_spec

package spectest

import (
	"testing"

	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
)

// RunSyncCommitteeAggProof is a stub for the alan_spec build. It exists so the
// spectest package test-compiles under -tags alan_spec (the symbol is referenced
// from the tag-free ssv_mapping_test.go). The alan spec-test entry point is not
// wired up, so this is never invoked at runtime under alan_spec; when the alan-side
// port lands, replace this with the real implementation.
func RunSyncCommitteeAggProof(t *testing.T, _ *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
	t.Skip("sync committee aggregator proof spec test is not wired up for the alan_spec build")
}
