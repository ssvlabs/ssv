package twoab

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// bodyA / bodyB are placeholder envelope-byte snippets; only their hash
// matters to the rate-limiter.
var (
	bodyA = []byte("envelope-content-A")
	bodyB = []byte("envelope-content-B")
)

func TestRateLimiter_DropsIdenticalBytes(t *testing.T) {
	rl := NewRateLimiter()
	const slot = phase0.Slot(1)
	const op = spectypes.OperatorID(2)

	// First observation of each kind — admitted.
	require.NoError(t, rl.AllowPhase1Bundle(slot, op, 0, bodyA))
	require.NoError(t, rl.AllowValueMsg(slot, op, bodyA))
	require.NoError(t, rl.AllowNoValueMsg(slot, op, bodyA))
	require.NoError(t, rl.AllowCommit(slot, op, bodyA))
	require.NoError(t, rl.AllowCertificate(slot, op, bodyA))

	// Same body redelivered — rejected for each kind.
	require.Error(t, rl.AllowPhase1Bundle(slot, op, 0, bodyA))
	require.Error(t, rl.AllowValueMsg(slot, op, bodyA))
	require.Error(t, rl.AllowNoValueMsg(slot, op, bodyA))
	require.Error(t, rl.AllowCommit(slot, op, bodyA))
	require.Error(t, rl.AllowCertificate(slot, op, bodyA))
}

// Distinct content from the same operator MUST be admitted so the protocol's
// equivocation paths fire (leader equivocation on a second Phase-1 bundle,
// Rule-6a / cross-side equivocation on a second KindValue / KindCommit).
func TestRateLimiter_AdmitsDistinctContent(t *testing.T) {
	rl := NewRateLimiter()
	const slot = phase0.Slot(1)
	const op = spectypes.OperatorID(2)

	require.NoError(t, rl.AllowPhase1Bundle(slot, op, 0, bodyA))
	require.NoError(t, rl.AllowPhase1Bundle(slot, op, 0, bodyB))
	require.NoError(t, rl.AllowValueMsg(slot, op, bodyA))
	require.NoError(t, rl.AllowValueMsg(slot, op, bodyB))
	require.NoError(t, rl.AllowCommit(slot, op, bodyA))
	require.NoError(t, rl.AllowCommit(slot, op, bodyB))
}

// Each kind has its own seen-map: the same (slot, op, body) admitted once per
// kind, never cross-rejected. Guards against accidentally sharing a map across
// kinds (which would drop a legitimate KindValue because an identical-bytes
// KindNoValue was seen first).
func TestRateLimiter_PerKindBucketsIndependent(t *testing.T) {
	rl := NewRateLimiter()
	const slot = phase0.Slot(1)
	const op = spectypes.OperatorID(2)

	require.NoError(t, rl.AllowPhase1Bundle(slot, op, 0, bodyA))
	require.NoError(t, rl.AllowValueMsg(slot, op, bodyA))
	require.NoError(t, rl.AllowNoValueMsg(slot, op, bodyA))
	require.NoError(t, rl.AllowCommit(slot, op, bodyA))
	require.NoError(t, rl.AllowCertificate(slot, op, bodyA))
}

func TestRateLimiter_DistinctKeysIndependent(t *testing.T) {
	rl := NewRateLimiter()
	// Same op + body, different slots / layers (bundle key carries layer).
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0, bodyA))
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 1, bodyA))
	require.NoError(t, rl.AllowPhase1Bundle(2, 2, 0, bodyA))
	// Different ops, same slot + body.
	require.NoError(t, rl.AllowValueMsg(1, 2, bodyA))
	require.NoError(t, rl.AllowValueMsg(1, 3, bodyA))
}

func TestRateLimiter_Forget_ClearsSlot(t *testing.T) {
	rl := NewRateLimiter()
	const slot = phase0.Slot(1)
	const op = spectypes.OperatorID(2)
	require.NoError(t, rl.AllowPhase1Bundle(slot, op, 0, bodyA))
	require.NoError(t, rl.AllowValueMsg(slot, op, bodyA))
	require.NoError(t, rl.AllowNoValueMsg(slot, op, bodyA))
	require.NoError(t, rl.AllowCommit(slot, op, bodyA))
	require.NoError(t, rl.AllowCertificate(slot, op, bodyA))

	rl.Forget(slot)

	// Same keys allowed again after Forget.
	require.NoError(t, rl.AllowPhase1Bundle(slot, op, 0, bodyA))
	require.NoError(t, rl.AllowValueMsg(slot, op, bodyA))
	require.NoError(t, rl.AllowNoValueMsg(slot, op, bodyA))
	require.NoError(t, rl.AllowCommit(slot, op, bodyA))
	require.NoError(t, rl.AllowCertificate(slot, op, bodyA))
}

// Entries past MaxAge are auto-evicted on the next Allow call, so memory
// doesn't leak even if Forget is never called.
func TestRateLimiter_TTL_EvictsExpiredEntries(t *testing.T) {
	rl := NewRateLimiterWithMaxAge(100 * time.Millisecond)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }

	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0, bodyA))
	require.NoError(t, rl.AllowValueMsg(1, 2, bodyA))
	require.NoError(t, rl.AllowNoValueMsg(1, 2, bodyA))
	require.NoError(t, rl.AllowCommit(1, 2, bodyA))
	require.NoError(t, rl.AllowCertificate(1, 2, bodyA))

	now = now.Add(200 * time.Millisecond) // past MaxAge

	// Same keys allowed again because the prior entries expired.
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0, bodyA))
	require.NoError(t, rl.AllowValueMsg(1, 2, bodyA))
	require.NoError(t, rl.AllowNoValueMsg(1, 2, bodyA))
	require.NoError(t, rl.AllowCommit(1, 2, bodyA))
	require.NoError(t, rl.AllowCertificate(1, 2, bodyA))

	// Only the fresh entries remain — each seen-map holds exactly one.
	rl.mu.Lock()
	require.Len(t, rl.bundleSeen, 1)
	require.Len(t, rl.valueSeen, 1)
	require.Len(t, rl.noValueSeen, 1)
	require.Len(t, rl.commitSeen, 1)
	require.Len(t, rl.certSeen, 1)
	rl.mu.Unlock()
}

func TestRateLimiter_TTL_DoesNotEvictFreshEntries(t *testing.T) {
	rl := NewRateLimiterWithMaxAge(1 * time.Hour)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }

	require.NoError(t, rl.AllowValueMsg(1, 2, bodyA))
	now = now.Add(10 * time.Minute) // well within MaxAge
	require.Error(t, rl.AllowValueMsg(1, 2, bodyA))
}

// Per-(slot, op) bucket cap: a byzantine streaming distinct content cannot
// inflate rate-limiter memory unboundedly. Exercised on KindValue (one of the
// shared allowOpKind kinds) including the error-message label.
func TestRateLimiter_BucketCap_RejectsBeyondMax(t *testing.T) {
	rl := NewRateLimiter()
	const slot = phase0.Slot(1)
	const op = spectypes.OperatorID(2)

	for k := 0; k < MaxDistinctPerOpSlot; k++ {
		require.NoError(t, rl.AllowValueMsg(slot, op, []byte{byte(k)}))
	}
	require.ErrorContains(t, rl.AllowValueMsg(slot, op, []byte{0xFF}), "too many distinct KindValues")

	// Different op at same slot: independent bucket, admit.
	require.NoError(t, rl.AllowValueMsg(slot, spectypes.OperatorID(3), []byte{0xFF}))
	// Different slot, same op: independent bucket, admit.
	require.NoError(t, rl.AllowValueMsg(phase0.Slot(2), op, []byte{0xFF}))

	// Forget clears the bucket; admissions resume.
	rl.Forget(slot)
	require.NoError(t, rl.AllowValueMsg(slot, op, []byte{0xFF}))
}

// Counter buckets must decrement when their Seen entries are TTL-evicted, so a
// re-fill after eviction succeeds (otherwise the count would stick at the cap
// and permanently reject that (slot, op)).
func TestRateLimiter_BucketCap_CountersDecrementOnEviction(t *testing.T) {
	rl := NewRateLimiterWithMaxAge(100 * time.Millisecond)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }
	const slot = phase0.Slot(1)
	const op = spectypes.OperatorID(2)

	for k := 0; k < MaxDistinctPerOpSlot; k++ {
		require.NoError(t, rl.AllowCommit(slot, op, []byte{byte(k)}))
	}
	rl.mu.Lock()
	require.Equal(t, MaxDistinctPerOpSlot, rl.commitCount[opBucket{slot: slot, op: op}])
	rl.mu.Unlock()

	require.Error(t, rl.AllowCommit(slot, op, []byte{0xFF})) // at cap

	now = now.Add(200 * time.Millisecond) // past TTL → next Allow evicts + decrements
	require.NoError(t, rl.AllowCommit(slot, op, []byte{0xFE}))

	// Room exists again up to the cap.
	for k := 0; k < MaxDistinctPerOpSlot-1; k++ {
		require.NoError(t, rl.AllowCommit(slot, op, []byte{0xA0, byte(k)}))
	}
	require.Error(t, rl.AllowCommit(slot, op, []byte{0xC0}))
}
