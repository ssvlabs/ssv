package validation

import (
	"sync"
	"testing"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// Tests for the per-validator Verifier cache (docs/OBFT-VALIDATION-VERIFIER-CACHE-PLAN.md).
//
// The cache is a validation-layer optimisation guarded by a content
// fingerprint: a committee/pub-share change flips the fingerprint and forces
// a rebuild, so a stale Verifier can never be served against the wrong
// shares. These tests lock in that safety property plus the hit/miss
// mechanics and concurrency-safety.

// withVerifierCaches initialises the two Verifier ttlcaches on an mv built by
// obftTestSetup (which constructs the struct directly, leaving them nil). Use
// this in tests that exercise the actual caching path rather than the
// nil-cache fallback.
func withVerifierCaches(t testing.TB, mv *messageValidator) {
	t.Helper()
	ttl := time.Minute
	mv.obftVerifiers = ttlcache.New(ttlcache.WithTTL[string, *cachedOBFTVerifier](ttl))
	mv.twoabVerifiers = ttlcache.New(ttlcache.WithTTL[string, *cachedTwoabVerifier](ttl))
}

// cloneShareWithChangedCommittee returns a deep-ish copy of share with the
// SAME ValidatorPubKey but one committee member's SharePubKey altered —
// modelling a reshare that keeps the validator identity but changes the
// committee's share pub-keys. The clone is independent so mutating it doesn't
// touch the original (which the cache may still reference).
func cloneShareWithChangedCommittee(t testing.TB, src *ssvtypes.SSVShare) *ssvtypes.SSVShare {
	t.Helper()
	require.NotEmpty(t, src.Committee, "source share must have a committee")

	dst := &ssvtypes.SSVShare{Share: src.Share}
	dst.Committee = make([]*spectypes.ShareMember, len(src.Committee))
	for i, m := range src.Committee {
		cp := *m
		cp.SharePubKey = append(spectypes.ShareValidatorPK(nil), m.SharePubKey...)
		dst.Committee[i] = &cp
	}
	// Flip a byte in the first member's SharePubKey → different fingerprint.
	dst.Committee[0].SharePubKey[0] ^= 0xFF
	return dst
}

// TestVerifierCache_OBFT_MissThenHit — the first lookup builds + caches a
// Verifier; the second lookup with the same share returns the SAME pointer
// (cache hit, no rebuild).
func TestVerifierCache_OBFT_MissThenHit(t *testing.T) {
	mv, _, share, _, _ := obftTestSetup(t)
	withVerifierCaches(t, mv)

	v1, err := mv.obftVerifierFor(share)
	require.NoError(t, err)
	require.NotNil(t, v1)

	v2, err := mv.obftVerifierFor(share)
	require.NoError(t, err)
	require.Same(t, v1, v2, "second lookup with unchanged share must hit the cache (same *Verifier)")
}

// TestVerifierCache_OBFT_CommitteeChangeRebuilds — the load-bearing safety
// test. A share with the SAME ValidatorPubKey (same cache key) but a CHANGED
// committee pub-share MUST NOT reuse the cached Verifier — the fingerprint
// mismatch forces a rebuild. Without this, a reshare would be verified
// against stale pub-shares.
func TestVerifierCache_OBFT_CommitteeChangeRebuilds(t *testing.T) {
	mv, _, share, _, _ := obftTestSetup(t)
	withVerifierCaches(t, mv)

	v1, err := mv.obftVerifierFor(share)
	require.NoError(t, err)

	changed := cloneShareWithChangedCommittee(t, share)
	require.Equal(t, share.ValidatorPubKey, changed.ValidatorPubKey,
		"clone must keep the same validator pubkey (same cache key)")

	v2, err := mv.obftVerifierFor(changed)
	require.NoError(t, err)
	require.NotSame(t, v1, v2,
		"a committee/pub-share change MUST force a rebuild, not reuse the stale Verifier")

	// And the changed share is now the cached one: re-looking it up hits.
	v3, err := mv.obftVerifierFor(changed)
	require.NoError(t, err)
	require.Same(t, v2, v3, "the rebuilt Verifier becomes the new cache entry")
}

// TestVerifierCache_OBFT_DistinctValidatorsDistinctEntries — two different
// validators (different ValidatorPubKey) get independent cache entries.
func TestVerifierCache_OBFT_DistinctValidatorsDistinctEntries(t *testing.T) {
	mv, _, share, _, _ := obftTestSetup(t)
	withVerifierCaches(t, mv)

	other := &ssvtypes.SSVShare{Share: share.Share}
	other.ValidatorPubKey = spectypes.ValidatorPK{0xDE, 0xAD, 0xBE, 0xEF}

	v1, err := mv.obftVerifierFor(share)
	require.NoError(t, err)
	v2, err := mv.obftVerifierFor(other)
	require.NoError(t, err)
	require.NotSame(t, v1, v2, "distinct validators must get distinct Verifiers")

	// Both remain cached independently.
	v1again, err := mv.obftVerifierFor(share)
	require.NoError(t, err)
	require.Same(t, v1, v1again)
}

// TestVerifierCache_OBFT_NilCacheFallback — a messageValidator built without
// New() (nil caches) must still return a working Verifier via the direct-
// construction fallback, never panicking. Mirrors the production safety net.
func TestVerifierCache_OBFT_NilCacheFallback(t *testing.T) {
	mv, _, share, _, _ := obftTestSetup(t) // does NOT init the caches
	require.Nil(t, mv.obftVerifiers, "obftTestSetup must leave the cache nil for this test")

	v, err := mv.obftVerifierFor(share)
	require.NoError(t, err)
	require.NotNil(t, v, "nil-cache fallback must still build a Verifier")
}

// TestVerifierCache_OBFT_ConcurrentLookupsAndVerifies — the load-bearing
// concurrency test (run under -race). Models the message-validation pool
// exactly: each goroutine looks up the (shared) cached Verifier AND calls a
// verify method on it. This drives concurrent access to the SHARED Verifier's
// F2 signing-root sub-cache (via VerifyPhase1Bundle → signingRootFor) and its
// read-only PubKeyShares map — the surface that must be safe for caching a
// single Verifier across goroutines to be sound.
//
// The bundle carries a real V (so signingRootFor actually runs and hits the
// shared srCache) with a placeholder σ_V; the verify is expected to FAIL
// (garbage sig), so we don't assert its result — only that the concurrent
// access is race-clean and never panics.
func TestVerifierCache_OBFT_ConcurrentLookupsAndVerifies(t *testing.T) {
	mv, _, share, _, clusterID := obftTestSetup(t)
	withVerifierCaches(t, mv)

	signer := share.Committee[0].Signer
	bundle := &obftcore.Phase1Bundle{
		ClusterID:   clusterID,
		OperatorID:  obftcore.OperatorID(signer),
		Height:      obftTestHeight(mv),
		Layer:       0,
		Value:       proposerCandidateV(),
		LeaderSigma: make([]byte, 96), // placeholder — verify will fail, that's fine
	}

	const goroutines = 16
	const calls = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < calls; i++ {
				v, err := mv.obftVerifierFor(share)
				if err != nil || v == nil {
					t.Errorf("concurrent obftVerifierFor failed: v=%v err=%v", v, err)
					return
				}
				// Exercise the shared Verifier's verify path concurrently —
				// drives the F2 srCache + read-only PubKeyShares map. Result
				// is ignored (placeholder sig); we only care about safety.
				_ = v.VerifyPhase1Bundle(bundle)
			}
		}()
	}
	wg.Wait()
}

// TestVerifierCache_Twoab_MissThenHit_AndRebuild — twoab twin of the OBFT
// hit + committee-change-rebuild tests, exercising twoabVerifierFor against
// the same share type.
func TestVerifierCache_Twoab_MissThenHit_AndRebuild(t *testing.T) {
	mv, _, share, _, _ := obftTestSetup(t)
	withVerifierCaches(t, mv)

	v1, err := mv.twoabVerifierFor(share)
	require.NoError(t, err)
	v2, err := mv.twoabVerifierFor(share)
	require.NoError(t, err)
	require.Same(t, v1, v2, "twoab: unchanged share must hit the cache")

	changed := cloneShareWithChangedCommittee(t, share)
	v3, err := mv.twoabVerifierFor(changed)
	require.NoError(t, err)
	require.NotSame(t, v1, v3, "twoab: committee change must force a rebuild")
}

// TestVerifierCache_Twoab_NilCacheFallback — twoab nil-cache fallback.
func TestVerifierCache_Twoab_NilCacheFallback(t *testing.T) {
	mv, _, share, _, _ := obftTestSetup(t)
	require.Nil(t, mv.twoabVerifiers)

	v, err := mv.twoabVerifierFor(share)
	require.NoError(t, err)
	require.NotNil(t, v)
}

// --- shareVerifierFingerprint unit tests ---

// TestShareVerifierFingerprint_Deterministic — the same share content hashes
// to the same fingerprint across calls.
func TestShareVerifierFingerprint_Deterministic(t *testing.T) {
	_, _, share, _, _ := obftTestSetup(t)
	fp1 := shareVerifierFingerprint(&share.Share)
	fp2 := shareVerifierFingerprint(&share.Share)
	require.Equal(t, fp1, fp2)
}

// TestShareVerifierFingerprint_OrderIndependent — committee member order in
// the slice must NOT change the fingerprint (the sort-by-Signer is
// load-bearing: the stored committee order is not guaranteed canonical).
func TestShareVerifierFingerprint_OrderIndependent(t *testing.T) {
	_, _, share, _, _ := obftTestSetup(t)
	require.GreaterOrEqual(t, len(share.Committee), 2, "need ≥2 members to permute")

	fpOriginal := shareVerifierFingerprint(&share.Share)

	reordered := &ssvtypes.SSVShare{Share: share.Share}
	reordered.Committee = make([]*spectypes.ShareMember, len(share.Committee))
	for i, m := range share.Committee {
		reordered.Committee[len(share.Committee)-1-i] = m // reverse order
	}
	fpReordered := shareVerifierFingerprint(&reordered.Share)

	require.Equal(t, fpOriginal, fpReordered,
		"member order must not affect the fingerprint (sort-by-Signer is load-bearing)")
}

// TestShareVerifierFingerprint_SensitiveToFields — changing any
// fingerprinted field (a member's SharePubKey, a member's Signer, or the
// ValidatorPubKey) changes the fingerprint.
func TestShareVerifierFingerprint_SensitiveToFields(t *testing.T) {
	_, _, share, _, _ := obftTestSetup(t)
	base := shareVerifierFingerprint(&share.Share)

	// (a) changed SharePubKey
	changedPK := cloneShareWithChangedCommittee(t, share)
	require.NotEqual(t, base, shareVerifierFingerprint(&changedPK.Share),
		"a changed SharePubKey must change the fingerprint")

	// (b) changed Signer
	changedSigner := &ssvtypes.SSVShare{Share: share.Share}
	changedSigner.Committee = make([]*spectypes.ShareMember, len(share.Committee))
	for i, m := range share.Committee {
		cp := *m
		changedSigner.Committee[i] = &cp
	}
	changedSigner.Committee[0].Signer ^= 0x01
	require.NotEqual(t, base, shareVerifierFingerprint(&changedSigner.Share),
		"a changed Signer must change the fingerprint")

	// (c) changed ValidatorPubKey
	changedVPK := &ssvtypes.SSVShare{Share: share.Share}
	changedVPK.ValidatorPubKey = spectypes.ValidatorPK{0x01, 0x02, 0x03}
	require.NotEqual(t, base, shareVerifierFingerprint(&changedVPK.Share),
		"a changed ValidatorPubKey must change the fingerprint")
}
