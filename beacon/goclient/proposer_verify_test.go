package goclient

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const testBeaconClientAddr = "http://bn-test:5052"

// newGoClientForVerifyTest builds a minimal *GoClient with only the fields required by
// verifyProposalParent. The observed core captures log entries the function emits so
// tests can assert on log fields without standing up a full client.
func newGoClientForVerifyTest(t *testing.T) (*GoClient, *observer.ObservedLogs) {
	t.Helper()
	core, observed := observer.New(zapcore.DebugLevel)
	return &GoClient{
		log:       zap.New(core),
		headCache: ttlcache.New[phase0.Slot, phase0.Root](),
	}, observed
}

func TestVerifyProposalParent_Slot0_ShortCircuitsWithoutLog(t *testing.T) {
	gc, observed := newGoClientForVerifyTest(t)
	proposal := spectestingutils.TestingBeaconBlockV(spec.DataVersionElectra)

	// Must not panic on uint64 underflow (slot - 1 when slot==0) and must not emit any log.
	gc.verifyProposalParent(t.Context(), gc.log, 0, proposal, testBeaconClientAddr)

	assert.Zero(t, observed.Len(), "slot==0 path must short-circuit before any log emission")
}

func TestVerifyProposalParent_CacheMissIsSilent(t *testing.T) {
	gc, observed := newGoClientForVerifyTest(t)
	proposal := spectestingutils.TestingBeaconBlockV(spec.DataVersionElectra)
	proposal.Electra.Block.Slot = 100

	// headCache is empty, so the parent slot lookup misses. Cache miss is metric-only,
	// no log entry should be emitted.
	gc.verifyProposalParent(t.Context(), gc.log, proposal.Electra.Block.Slot, proposal, testBeaconClientAddr)

	assert.Zero(t, observed.Len(), "cache-miss path must not emit any log")
}

func TestVerifyProposalParent_MatchIsSilent(t *testing.T) {
	gc, observed := newGoClientForVerifyTest(t)
	proposal := spectestingutils.TestingBeaconBlockV(spec.DataVersionElectra)
	proposal.Electra.Block.Slot = 100

	// Pre-seed the cache with the parent root that the proposal carries — this is the
	// match path which is also metric-only.
	gc.headCache.Set(proposal.Electra.Block.Slot-1, proposal.Electra.Block.ParentRoot, ttlcache.NoTTL)

	gc.verifyProposalParent(t.Context(), gc.log, proposal.Electra.Block.Slot, proposal, testBeaconClientAddr)

	assert.Zero(t, observed.Len(), "match path must not emit any log")
}

func TestVerifyProposalParent_MismatchLogsBeaconClientField(t *testing.T) {
	gc, observed := newGoClientForVerifyTest(t)
	proposal := spectestingutils.TestingBeaconBlockV(spec.DataVersionElectra)
	proposal.Electra.Block.Slot = 100

	// Cache holds a different root for slot-1 than what the proposal references —
	// triggers the mismatch path which logs at Info with the beacon_client field.
	cachedRoot := phase0.Root{0xAA}
	require.NotEqual(t, cachedRoot, proposal.Electra.Block.ParentRoot)
	gc.headCache.Set(proposal.Electra.Block.Slot-1, cachedRoot, ttlcache.NoTTL)

	gc.verifyProposalParent(t.Context(), gc.log, proposal.Electra.Block.Slot, proposal, testBeaconClientAddr)

	require.Equal(t, 1, observed.Len(), "mismatch path must emit exactly one log entry")
	entry := observed.All()[0]
	assert.Equal(t, zapcore.InfoLevel, entry.Level, "mismatch log must be Info (was Warn pre-PR)")
	assert.Equal(t, "proposal parent root mismatch detected", entry.Message)

	fields := entry.ContextMap()
	beaconClient, ok := fields["beacon_client"]
	require.True(t, ok, "mismatch log must include beacon_client field")
	assert.Equal(t, testBeaconClientAddr, beaconClient)
}
