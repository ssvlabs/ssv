package obft

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// Tests for the proposerSigner's signing-root cache (audit finding F2),
// which delegates to the shared proposersig.Cache. See
// docs/OBFT-PERFORMANCE-AUDIT-PLAN.md §F2 and the proposersig package.
//
// The cache amortises signingRootFor — the SSZ-unmarshal + tree-root +
// domain compute step costing ~100 µs / ~336 allocs per call on a 17 KB
// block (B2) — across the same V's many BLS sign / verify / aggregate-
// verify invocations within a slot. Cache key is sha256(V); the V's
// SSZ-marshaled bytes carry the block's slot field so distinct (slot,
// fork) pairs produce distinct keys — fork-safe in practice.
//
// These tests drive the proposerSigner (which delegates to the shared
// proposersig.Cache) and observe cache size via the exported Cache.Len().
// They live here, in the base adapter, as the single home for the shared
// mechanism's tests — the 2abOBFT proposerSigner delegates to the same
// proposersig.Cache, so its caching behaviour is covered transitively
// (its own wiring is smoke-checked in twoab/proposer_signer_test.go). The
// twoab copy of these tests was removed when the cache was extracted.
// End-to-end correctness is also covered by the runner integration +
// consensustest stress suites.

// TestProposerSigner_F2_SigningRootCache_MissThenHit — first signingRootFor
// call populates the cache; a second call with the same V finds the existing
// entry. The signing root returned is byte-identical between calls.
func TestProposerSigner_F2_SigningRootCache_MissThenHit(t *testing.T) {
	v := makeBenchV(t, 0)
	ps := newTestProposerSigner(t, &recordingBatchSigner{batchReturns: true})

	require.Equal(t, 0, ps.sr.Len(), "cache must start empty")
	sr1, err := ps.signingRootFor(v)
	require.NoError(t, err)
	require.Equal(t, 1, ps.sr.Len(), "first call must populate exactly one entry")
	sr2, err := ps.signingRootFor(v)
	require.NoError(t, err)
	require.Equal(t, 1, ps.sr.Len(), "second call with same V must hit cache, no new entry")
	require.Equal(t, sr1, sr2, "cached signing root must equal the fresh-compute result")
}

// TestProposerSigner_F2_SigningRootCache_FailureNotCached — when the
// uncached translation fails (bad V bytes that don't decode), the cache
// stays empty. Future calls re-attempt the decode rather than serving a
// poisoned cache entry.
func TestProposerSigner_F2_SigningRootCache_FailureNotCached(t *testing.T) {
	ps := newTestProposerSigner(t, &recordingBatchSigner{batchReturns: true})
	badV := []byte("not-a-valid-candidate")

	_, err := ps.signingRootFor(badV)
	require.Error(t, err, "bad V must produce an error")
	require.Equal(t, 0, ps.sr.Len(), "failed signingRootFor MUST NOT populate the cache")
}

// TestProposerSigner_F2_SigningRootCache_EmptyValueNotCached — the empty
// V bytes branch bypasses the cache and falls through to the uncached
// translation (which then errors via DecodeCandidate), so the all-zero
// key never pollutes future lookups.
func TestProposerSigner_F2_SigningRootCache_EmptyValueNotCached(t *testing.T) {
	ps := newTestProposerSigner(t, &recordingBatchSigner{batchReturns: true})

	_, err := ps.signingRootFor(nil)
	require.Error(t, err, "empty V must error via the underlying DecodeCandidate")
	require.Equal(t, 0, ps.sr.Len(), "empty V MUST NOT populate the cache")
}

// TestProposerSigner_F2_SigningRootCache_DistinctVSeparateEntries — two
// distinct V's produce two distinct cache entries. There is no key
// collision; the sha256(V) keying separates them.
func TestProposerSigner_F2_SigningRootCache_DistinctVSeparateEntries(t *testing.T) {
	ps := newTestProposerSigner(t, &recordingBatchSigner{batchReturns: true})
	vSmall := makeBenchV(t, 0)
	vLarge := makeBenchV(t, 32)

	_, err := ps.signingRootFor(vSmall)
	require.NoError(t, err)
	_, err = ps.signingRootFor(vLarge)
	require.NoError(t, err)
	require.Equal(t, 2, ps.sr.Len(), "distinct V's must produce distinct cache entries")
}

// TestProposerSigner_F2_VerifyPartialBatch_SameV_OneCacheEntry — F4's
// σ-walk batches every tuple at the SAME V; F2's cache means the loop
// in VerifyPartialBatch translates V once and serves the remaining N-1
// translations from the map. We assert by inspecting cache size after a
// batch — it must be exactly 1 entry, not N.
func TestProposerSigner_F2_VerifyPartialBatch_SameV_OneCacheEntry(t *testing.T) {
	v := makeBenchV(t, 0)
	const n = 5
	pubs := make([][]byte, n)
	msgs := make([][]byte, n)
	sigs := make([]obftcore.Signature, n)
	for i := 0; i < n; i++ {
		pubs[i] = []byte{byte(i + 1)}
		msgs[i] = v
		sigs[i] = obftcore.Signature{byte(i)}
	}

	inner := &recordingBatchSigner{batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	require.True(t, ps.VerifyPartialBatch(pubs, msgs, sigs))
	require.Equal(t, 1, inner.batchCalls)
	require.Equal(t, 1, ps.sr.Len(),
		"N tuples sharing one V must produce exactly ONE cache entry (F2 collapses the translates)")
}

// TestProposerSigner_F2_SigningRootCache_ConcurrentSameV_RaceClean —
// concurrent signingRootFor calls on the same V from many goroutines
// (mirrors the production validation-layer pool + σ-walk single-instance
// concurrent access) must be race-clean and end with one cache entry.
func TestProposerSigner_F2_SigningRootCache_ConcurrentSameV_RaceClean(t *testing.T) {
	v := makeBenchV(t, 0)
	ps := newTestProposerSigner(t, &recordingBatchSigner{batchReturns: true})

	const goroutines = 16
	const calls = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < calls; i++ {
				if _, err := ps.signingRootFor(v); err != nil {
					t.Errorf("signingRootFor failed under concurrency: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, ps.sr.Len(),
		"concurrent populates for the same V must converge to one entry")
}

// TestProposerSigner_F2_SigningRootCache_SignAndVerifyShareCache — the cache
// is hit across method boundaries: SignPartial populates → VerifyPartial
// hits → VerifyAggregate hits → VerifyPartialBatch hits. All four runner-
// signer methods that take V go through the same memoised signingRootFor.
func TestProposerSigner_F2_SigningRootCache_SignAndVerifyShareCache(t *testing.T) {
	v := makeBenchV(t, 0)

	// Use a mock inner that does not check signature semantics; we only
	// care about the cache state, not the BLS verification outcome.
	inner := &recordingMockAllOps{verifyReturns: true, batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	// SignPartial → populates.
	_, _ = ps.SignPartial(v)
	require.Equal(t, 1, ps.sr.Len(), "SignPartial must populate the cache")

	// VerifyPartial → hits cache, no new entry.
	require.True(t, ps.VerifyPartial([]byte{1}, v, obftcore.Signature{0xa}))
	require.Equal(t, 1, ps.sr.Len(), "VerifyPartial must hit cache, not add")

	// VerifyAggregate → hits cache, no new entry.
	require.True(t, ps.VerifyAggregate([]byte{2}, v, obftcore.Signature{0xb}))
	require.Equal(t, 1, ps.sr.Len(), "VerifyAggregate must hit cache, not add")

	// VerifyPartialBatch with single V → hits cache, no new entry.
	require.True(t, ps.VerifyPartialBatch(
		[][]byte{{1}, {2}}, [][]byte{v, v}, []obftcore.Signature{{0xc}, {0xd}}))
	require.Equal(t, 1, ps.sr.Len(), "VerifyPartialBatch must hit cache, not add")
}

// recordingMockAllOps is a fuller mock than recordingBatchSigner — it also
// stubs SignPartial / VerifyPartial / VerifyAggregate so the
// SignAndVerifyShareCache test can exercise every entry point that takes V.
// Returns synthetic responses; the inner's BLS realism doesn't matter for
// what's being asserted (cache state).
type recordingMockAllOps struct {
	verifyReturns bool
	batchReturns  bool
}

func (m *recordingMockAllOps) SignPartial([]byte) (obftcore.Signature, error) {
	return obftcore.Signature{0x99}, nil
}
func (m *recordingMockAllOps) AggregatePartials(map[obftcore.OperatorID]obftcore.Signature) (obftcore.Signature, error) {
	return obftcore.Signature{0x88}, nil
}
func (m *recordingMockAllOps) VerifyPartial([]byte, []byte, obftcore.Signature) bool {
	return m.verifyReturns
}
func (m *recordingMockAllOps) VerifyAggregate([]byte, []byte, obftcore.Signature) bool {
	return m.verifyReturns
}
func (m *recordingMockAllOps) VerifyPartialBatch([][]byte, [][]byte, []obftcore.Signature) bool {
	return m.batchReturns
}
