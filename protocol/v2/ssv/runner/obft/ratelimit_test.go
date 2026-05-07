package obft

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// bodyA / bodyB are placeholder envelope-byte snippets for tests; only
// their hash matters to the rate-limiter.
var (
	bodyA = []byte("envelope-content-A")
	bodyB = []byte("envelope-content-B")
)

func TestRateLimiter_DropsIdenticalBytes(t *testing.T) {
	rl := NewRateLimiter()

	// First observation — admitted.
	require.NoError(t, rl.AllowPhase1Bundle(1, spectypes.OperatorID(2), 0, bodyA))
	require.NoError(t, rl.AllowCommit(1, spectypes.OperatorID(2), bodyA))
	require.NoError(t, rl.AllowCertificate(1, spectypes.OperatorID(2), bodyA))

	// Same body redelivered — rejected.
	require.Error(t, rl.AllowPhase1Bundle(1, spectypes.OperatorID(2), 0, bodyA))
	require.Error(t, rl.AllowCommit(1, spectypes.OperatorID(2), bodyA))
	require.Error(t, rl.AllowCertificate(1, spectypes.OperatorID(2), bodyA))
}

// CRITICAL #2 regression: distinct content from the same operator MUST be
// admitted so the protocol's equivocation paths fire (Rule 2 on second
// distinct Phase-1 bundle, Rule 3 on second distinct KindCommit).
func TestRateLimiter_AdmitsDistinctContent(t *testing.T) {
	rl := NewRateLimiter()

	// First Phase-1 bundle from op2.
	require.NoError(t, rl.AllowPhase1Bundle(1, spectypes.OperatorID(2), 0, bodyA))
	// Second distinct Phase-1 bundle from same op (byzantine equivocation)
	// — must be admitted so Rule 2 detection fires at the protocol layer.
	require.NoError(t, rl.AllowPhase1Bundle(1, spectypes.OperatorID(2), 0, bodyB))

	// First Commit from op2.
	require.NoError(t, rl.AllowCommit(1, spectypes.OperatorID(2), bodyA))
	// Second distinct Commit (Rule 3 cross-onion equivocation) — must
	// reach protocol layer for attribution.
	require.NoError(t, rl.AllowCommit(1, spectypes.OperatorID(2), bodyB))
}

func TestRateLimiter_DistinctKeysIndependent(t *testing.T) {
	rl := NewRateLimiter()
	// Same op + body, different slots / layers.
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0, bodyA))
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 1, bodyA))
	require.NoError(t, rl.AllowPhase1Bundle(2, 2, 0, bodyA))
	// Different ops, same slot + body.
	require.NoError(t, rl.AllowCommit(1, 2, bodyA))
	require.NoError(t, rl.AllowCommit(1, 3, bodyA))
}

func TestRateLimiter_Forget_ClearsSlot(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0, bodyA))
	require.NoError(t, rl.AllowCommit(1, 2, bodyA))
	require.NoError(t, rl.AllowCertificate(1, 2, bodyA))
	rl.Forget(1)
	// Same keys allowed again after Forget.
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0, bodyA))
	require.NoError(t, rl.AllowCommit(1, 2, bodyA))
	require.NoError(t, rl.AllowCertificate(1, 2, bodyA))
}

// G4 regression: entries past MaxAge are auto-evicted on the next Allow call,
// so memory doesn't leak even if Forget is never called.
func TestRateLimiter_TTL_EvictsExpiredEntries(t *testing.T) {
	rl := NewRateLimiterWithMaxAge(100 * time.Millisecond)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }

	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0, bodyA))
	require.NoError(t, rl.AllowCommit(1, 2, bodyA))
	require.NoError(t, rl.AllowCertificate(1, 2, bodyA))

	// Advance time past MaxAge.
	now = now.Add(200 * time.Millisecond)

	// Same keys allowed again because the prior entries expired.
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0, bodyA))
	require.NoError(t, rl.AllowCommit(1, 2, bodyA))
	require.NoError(t, rl.AllowCertificate(1, 2, bodyA))

	// Ensure underlying map size is bounded — only the new entries remain.
	rl.mu.Lock()
	require.Len(t, rl.bundleSeen, 1)
	require.Len(t, rl.commitSeen, 1)
	require.Len(t, rl.certSeen, 1)
	rl.mu.Unlock()
}

func TestRateLimiter_TTL_DoesNotEvictFreshEntries(t *testing.T) {
	rl := NewRateLimiterWithMaxAge(1 * time.Hour)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }

	require.NoError(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2), bodyA))
	now = now.Add(10 * time.Minute) // well within MaxAge
	require.Error(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2), bodyA))
}

// Per-(slot, op) bucket cap: a byzantine streaming distinct content cannot
// inflate rate-limiter memory unboundedly. Beyond MaxDistinctPerOpSlot,
// further distinct admissions are rejected.
func TestRateLimiter_BucketCap_RejectsBeyondMax(t *testing.T) {
	rl := NewRateLimiter()

	// Admit MaxDistinctPerOpSlot distinct commits.
	for k := 0; k < MaxDistinctPerOpSlot; k++ {
		body := []byte{byte(k)}
		require.NoError(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2), body))
	}

	// Next distinct content from same (slot, op) — over cap, rejected.
	require.ErrorContains(t,
		rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2), []byte{0xFF}),
		"too many distinct KindCommits")

	// Different op at same slot: independent bucket, admit.
	require.NoError(t,
		rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(3), []byte{0xFF}))

	// Different slot, same op: independent bucket, admit.
	require.NoError(t,
		rl.AllowCommit(phase0.Slot(2), spectypes.OperatorID(2), []byte{0xFF}))

	// Forget(slot=1) clears the bucket; admissions resume.
	rl.Forget(phase0.Slot(1))
	require.NoError(t,
		rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2), []byte{0xFF}))
}

// Per-(slot, op, layer) bucket cap for phase-1 bundles: separate buckets
// per layer.
func TestRateLimiter_BucketCap_BundleSeparatesLayers(t *testing.T) {
	rl := NewRateLimiter()

	// Fill layer-0 bucket for op2 at slot 1.
	for k := 0; k < MaxDistinctPerOpSlot; k++ {
		body := []byte{byte(k)}
		require.NoError(t, rl.AllowPhase1Bundle(phase0.Slot(1), spectypes.OperatorID(2), 0, body))
	}
	require.ErrorContains(t,
		rl.AllowPhase1Bundle(phase0.Slot(1), spectypes.OperatorID(2), 0, []byte{0xFF}),
		"too many distinct phase-1 bundles")

	// Layer-1 is a separate bucket; first admit succeeds.
	require.NoError(t,
		rl.AllowPhase1Bundle(phase0.Slot(1), spectypes.OperatorID(2), 1, []byte{0xFF}))
}

// Regression: counter buckets must decrement when their corresponding Seen
// entries are TTL-evicted, so a re-fill after eviction succeeds. Without
// the decrement, a bucket's count would stay at MaxDistinctPerOpSlot and
// permanently reject admissions for that (slot, op) pair.
func TestRateLimiter_BucketCap_CountersDecrementOnEviction(t *testing.T) {
	rl := NewRateLimiterWithMaxAge(100 * time.Millisecond)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }

	// Fill the commit bucket to the cap.
	for k := 0; k < MaxDistinctPerOpSlot; k++ {
		require.NoError(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2), []byte{byte(k)}))
	}
	rl.mu.Lock()
	require.Equal(t, MaxDistinctPerOpSlot, rl.commitCount[opBucket{slot: 1, op: 2}],
		"counter must reach cap after fill")
	rl.mu.Unlock()

	// Cap reached → next distinct admission rejects.
	require.Error(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2), []byte{0xFF}))

	// Advance past TTL; the next Allow* triggers eviction, which must
	// decrement counters in lockstep with Seen-map evictions.
	now = now.Add(200 * time.Millisecond)
	require.NoError(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2), []byte{0xFE}))

	// After eviction + the one fresh admit, counter is 1 (or the bucket was
	// fully reaped and recreated). Either way, more admissions should fit
	// up to the cap.
	for k := 0; k < MaxDistinctPerOpSlot-1; k++ {
		// Use distinct bytes that don't collide with the {0xFE} we just admitted.
		require.NoError(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2),
			[]byte{0xA0, byte(k)}))
	}
	// Now we've admitted MaxDistinctPerOpSlot post-eviction; next one rejects.
	require.Error(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2), []byte{0xC0}))
}
