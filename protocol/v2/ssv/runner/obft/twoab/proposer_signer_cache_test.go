package twoab

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Tests for the twoab proposerSigner srCache (audit finding F2). Mirror of
// protocol/v2/ssv/runner/obft/proposer_signer_cache_test.go. See
// docs/OBFT-PERFORMANCE-AUDIT-PLAN.md §F2 for the design.

// TestProposerSigner_F2_SigningRootCache_MissThenHit — mirror of base.
func TestProposerSigner_F2_SigningRootCache_MissThenHit(t *testing.T) {
	v := makeTestV(t)
	ps := newTestProposerSigner(t, &recordingBatchSigner{batchReturns: true})

	require.Empty(t, ps.srCache, "cache must start empty")
	sr1, err := ps.signingRootFor(v)
	require.NoError(t, err)
	require.Len(t, ps.srCache, 1)
	sr2, err := ps.signingRootFor(v)
	require.NoError(t, err)
	require.Len(t, ps.srCache, 1)
	require.Equal(t, sr1, sr2)
}

// TestProposerSigner_F2_SigningRootCache_FailureNotCached — bad V doesn't
// populate the cache; future calls re-attempt.
func TestProposerSigner_F2_SigningRootCache_FailureNotCached(t *testing.T) {
	ps := newTestProposerSigner(t, &recordingBatchSigner{batchReturns: true})
	badV := []byte("not-a-valid-candidate")

	_, err := ps.signingRootFor(badV)
	require.Error(t, err)
	require.Empty(t, ps.srCache, "failed signingRootFor MUST NOT populate the cache")
}

// TestProposerSigner_F2_SigningRootCache_EmptyValueNotCached — the empty V
// branch bypasses the cache.
func TestProposerSigner_F2_SigningRootCache_EmptyValueNotCached(t *testing.T) {
	ps := newTestProposerSigner(t, &recordingBatchSigner{batchReturns: true})

	_, err := ps.signingRootFor(nil)
	require.Error(t, err)
	require.Empty(t, ps.srCache)
}

// TestProposerSigner_F2_SigningRootCache_DistinctVSeparateEntries — two
// distinct V's produce two distinct cache entries. sha256(V) keys do not
// collide; the slot field embedded in each block's SSZ disambiguates.
func TestProposerSigner_F2_SigningRootCache_DistinctVSeparateEntries(t *testing.T) {
	ps := newTestProposerSigner(t, &recordingBatchSigner{batchReturns: true})
	vA := makeTestVForSlot(t, 12345)
	vB := makeTestVForSlot(t, 67890)

	_, err := ps.signingRootFor(vA)
	require.NoError(t, err)
	_, err = ps.signingRootFor(vB)
	require.NoError(t, err)
	require.Len(t, ps.srCache, 2, "distinct V's must produce distinct cache entries")
}

// TestProposerSigner_F2_VerifyPartialBatch_SameV_OneCacheEntry — F4 σ-walk
// batches sharing one V translate exactly once.
func TestProposerSigner_F2_VerifyPartialBatch_SameV_OneCacheEntry(t *testing.T) {
	v := makeTestV(t)
	const n = 5
	pubs := make([][]byte, n)
	msgs := make([][]byte, n)
	sigs := make([]twoabcore.Signature, n)
	for i := 0; i < n; i++ {
		pubs[i] = []byte{byte(i + 1)}
		msgs[i] = v
		sigs[i] = twoabcore.Signature{byte(i)}
	}

	inner := &recordingBatchSigner{batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	require.True(t, ps.VerifyPartialBatch(pubs, msgs, sigs))
	require.Equal(t, 1, inner.batchCalls)
	require.Len(t, ps.srCache, 1)
}

// TestProposerSigner_F2_SigningRootCache_ConcurrentSameV_RaceClean — many
// goroutines on the same V converge to one entry.
func TestProposerSigner_F2_SigningRootCache_ConcurrentSameV_RaceClean(t *testing.T) {
	v := makeTestV(t)
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
	require.Len(t, ps.srCache, 1)
}

// TestProposerSigner_F2_SigningRootCache_SignAndVerifyShareCache — the cache
// is hit across SignPartial / VerifyPartial / VerifyAggregate /
// VerifyPartialBatch. All four wrapper methods that take V go through the
// same memoised signingRootFor.
func TestProposerSigner_F2_SigningRootCache_SignAndVerifyShareCache(t *testing.T) {
	v := makeTestV(t)

	inner := &recordingMockAllOps{verifyReturns: true, batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	_, _ = ps.SignPartial(v)
	require.Len(t, ps.srCache, 1, "SignPartial must populate srCache")

	require.True(t, ps.VerifyPartial([]byte{1}, v, twoabcore.Signature{0xa}))
	require.Len(t, ps.srCache, 1, "VerifyPartial must hit cache, not add")

	require.True(t, ps.VerifyAggregate([]byte{2}, v, twoabcore.Signature{0xb}))
	require.Len(t, ps.srCache, 1, "VerifyAggregate must hit cache, not add")

	require.True(t, ps.VerifyPartialBatch(
		[][]byte{{1}, {2}}, [][]byte{v, v}, []twoabcore.Signature{{0xc}, {0xd}}))
	require.Len(t, ps.srCache, 1, "VerifyPartialBatch must hit cache, not add")
}

// recordingMockAllOps stubs every Signer method; companion to
// recordingBatchSigner (which panics on unexpected calls). Used by the
// cross-method-cache test where multiple Signer methods must succeed.
type recordingMockAllOps struct {
	verifyReturns bool
	batchReturns  bool
}

func (m *recordingMockAllOps) SignPartial([]byte) (twoabcore.Signature, error) {
	return twoabcore.Signature{0x99}, nil
}

func (m *recordingMockAllOps) AggregatePartials(map[twoabcore.OperatorID]twoabcore.Signature) (twoabcore.Signature, error) {
	return twoabcore.Signature{0x88}, nil
}

func (m *recordingMockAllOps) VerifyPartial([]byte, []byte, twoabcore.Signature) bool {
	return m.verifyReturns
}

func (m *recordingMockAllOps) VerifyAggregate([]byte, []byte, twoabcore.Signature) bool {
	return m.verifyReturns
}

func (m *recordingMockAllOps) VerifyPartialBatch([][]byte, [][]byte, []twoabcore.Signature) bool {
	return m.batchReturns
}
