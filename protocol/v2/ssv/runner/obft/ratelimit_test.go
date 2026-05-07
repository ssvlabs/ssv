package obft

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter_AllowsFirstRejectsSecond(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowPhase1Bundle(1, spectypes.OperatorID(2), 0))
	require.Error(t, rl.AllowPhase1Bundle(1, spectypes.OperatorID(2), 0))

	require.NoError(t, rl.AllowCommit(1, spectypes.OperatorID(2)))
	require.Error(t, rl.AllowCommit(1, spectypes.OperatorID(2)))

	require.NoError(t, rl.AllowCertificate(1, spectypes.OperatorID(2)))
	require.Error(t, rl.AllowCertificate(1, spectypes.OperatorID(2)))
}

func TestRateLimiter_DistinctKeysIndependent(t *testing.T) {
	rl := NewRateLimiter()
	// Same op, different slots / layers.
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0))
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 1))
	require.NoError(t, rl.AllowPhase1Bundle(2, 2, 0))
	// Different ops, same slot.
	require.NoError(t, rl.AllowCommit(1, 2))
	require.NoError(t, rl.AllowCommit(1, 3))
}

func TestRateLimiter_Forget_ClearsSlot(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0))
	require.NoError(t, rl.AllowCommit(1, 2))
	require.NoError(t, rl.AllowCertificate(1, 2))
	rl.Forget(1)
	// Same keys allowed again after Forget.
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0))
	require.NoError(t, rl.AllowCommit(1, 2))
	require.NoError(t, rl.AllowCertificate(1, 2))
}

// G4 regression: entries past MaxAge are auto-evicted on the next Allow call,
// so memory doesn't leak even if Forget is never called.
func TestRateLimiter_TTL_EvictsExpiredEntries(t *testing.T) {
	rl := NewRateLimiterWithMaxAge(100 * time.Millisecond)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }

	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0))
	require.NoError(t, rl.AllowCommit(1, 2))
	require.NoError(t, rl.AllowCertificate(1, 2))

	// Advance time past MaxAge.
	now = now.Add(200 * time.Millisecond)

	// Same keys allowed again because the prior entries expired.
	require.NoError(t, rl.AllowPhase1Bundle(1, 2, 0))
	require.NoError(t, rl.AllowCommit(1, 2))
	require.NoError(t, rl.AllowCertificate(1, 2))

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

	require.NoError(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2)))
	now = now.Add(10 * time.Minute) // well within MaxAge
	require.Error(t, rl.AllowCommit(phase0.Slot(1), spectypes.OperatorID(2)))
}
