package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the per-Instance verify-cache (audit finding F1). Mirrors
// obft/base/phase3_verifycache_test.go. The safety invariant in short form
// is "cache populate is gated
// EXCLUSIVELY by signer.VerifyPartial just returning true on that exact
// (op, layer, value, partial-bytes) tuple, and value-binding means a cache
// hit can never let a partial contribute to a V it doesn't sign".
//
// twoab's only Resolve-side verify is at phase3.go's L_k>0 σ-walk —
// L_0 σ partials are pre-verified at observation (verifyAndPoolL0Partial)
// and Resolve reads sigmaPool directly. So the cache only buys anything
// at L_k>0, where the chained-encrypted partial is first decrypted and
// verified inside Resolve. These tests exercise the helpers directly
// (sufficient for the safety properties at the cache-key level); end-to-
// end correctness is covered by the existing TwoabHealthy_n4 / consensustest
// stress suites which all pass with the F1 cache in place.

// TestTwoab_F1_VerifyOrCached_MissThenHit — direct test of verifyOrCached's
// miss-then-hit transition: first call runs the BLS verify and populates the
// cache; second call hits the cache and skips the BLS verify. This is the
// behaviour twoab's phase3.go L_k>0 σ-walk relies on for opportunistic re-
// Resolves to skip redundant work.
//
// Also checks the negative-cache safety property: a tampered partial-byte
// sequence at the same (op, layer, value) must NOT pass via cache hit on
// the real partial — it falls through to a fresh verify and gets rejected.
func TestTwoab_F1_VerifyOrCached_MissThenHit(t *testing.T) {
	s := newSim(t, 4)
	v := []byte("V_for_op2")
	receiver := s.instances[3]
	pubShare := receiver.pubKeyShares[2]

	signer := NewStubSigner(s.cfg.QV(), s.pubShares[2])
	partial, err := signer.SignPartial(v)
	require.NoError(t, err)

	require.False(t, receiver.alreadyVerified(2, 1, v, partial), "cache empty pre-call")
	require.True(t, receiver.verifyOrCached(2, 1, pubShare, v, partial),
		"first call should verify and return true")
	require.True(t, receiver.alreadyVerified(2, 1, v, partial),
		"verifyOrCached must populate the cache on first success")
	require.True(t, receiver.verifyOrCached(2, 1, pubShare, v, partial),
		"second call should hit the cache and still return true")

	// Tampered partial must fail full verify and NOT populate.
	tampered := append([]byte{0xFF}, partial...)
	require.False(t, receiver.verifyOrCached(2, 1, pubShare, v, tampered),
		"tampered partial must fail full verify (cache miss → BLS verify → false)")
	require.False(t, receiver.alreadyVerified(2, 1, v, tampered),
		"failed verify must not populate the cache for the tampered bytes")
}

// TestTwoab_F1_FailedVerifyNotCached — a fresh verify that fails MUST NOT
// populate the cache. Direct exercise of the safety invariant: without
// this, a malformed partial could later pass through Resolve via a phantom
// cache hit on the same byte sequence.
func TestTwoab_F1_FailedVerifyNotCached(t *testing.T) {
	s := newSim(t, 4)
	v := []byte("V_for_op2")
	receiver := s.instances[3]
	pubShare := receiver.pubKeyShares[2]

	signer := NewStubSigner(s.cfg.QV(), s.pubShares[2])
	real, err := signer.SignPartial(v)
	require.NoError(t, err)
	malformed := append([]byte{0xFF}, real...)

	require.False(t, receiver.verifyOrCached(2, 1, pubShare, v, malformed),
		"malformed partial must fail verify")
	require.False(t, receiver.alreadyVerified(2, 1, v, malformed),
		"failed BLS verify MUST NOT populate the cache (safety invariant)")
}

// TestTwoab_F1_EquivocationDistinctPartialsCachedIndependently — a byzantine
// emitting two distinct partial-byte sequences at the same (op, layer, value)
// MUST cache them independently. A cache hit on one partial must NOT let a
// different partial bypass verify. partialRoot in verifyCacheKey is the
// load-bearing disambiguator for this byzantine case.
func TestTwoab_F1_EquivocationDistinctPartialsCachedIndependently(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]

	v := []byte("V_for_op2_at_L_1")
	sigA := []byte{0x10, 0x01, 0x02, 0x03}
	sigB := []byte{0x10, 0x99, 0x98, 0x97}

	receiver.markVerified(2, 1, v, sigA)
	require.True(t, receiver.alreadyVerified(2, 1, v, sigA), "A must be cached")
	require.False(t, receiver.alreadyVerified(2, 1, v, sigB),
		"distinct partial B must NOT inherit A's cache entry")
}

// TestTwoab_F1_ValueBoundCacheKey_NoCrossVLeak — the load-bearing safety
// property that motivates including valueRoot in the cache key.
//
// Attack the cache key WITHOUT valueRoot would admit: byzantine emits two
// L_k>0 onion entries from the same (op, layer) — entry A claims V_a and
// decrypts to σ_a (the leader's real σ on V_a), entry B claims V_b but
// decrypts to the same σ_a bytes. Resolve walks entry A first → verify σ_a
// against V_a passes → cache populates. Walk entry B → cache hit on
// (op, layer, sha256(σ_a)) → skip verify → addToGroup(V_b, op, σ_a) admits
// σ_a to V_b's σ-pool incorrectly.
//
// valueRoot in the cache key blocks this: A's populate key is
// (op, layer, sha256(V_a), sha256(σ_a)); B's lookup is
// (op, layer, sha256(V_b), sha256(σ_a)). Different valueRoot → cache miss
// → full verify → σ_a against V_b → fails → no contribution.
//
// twoab's L_k>0 walk has the same attack surface as base's, so the same
// safety property is load-bearing here. This test exercises the helpers
// directly (rather than threading through an L_k>0 Resolve walk) because
// the property is at the cache-key level.
func TestTwoab_F1_ValueBoundCacheKey_NoCrossVLeak(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]

	vA := []byte("V_a")
	vB := []byte("V_b")
	sig := []byte("partial_sig_bytes_for_sigma_a")

	receiver.markVerified(2, 2, vA, sig)
	require.True(t, receiver.alreadyVerified(2, 2, vA, sig),
		"populate must produce a cache hit for the SAME (value, partial)")
	require.False(t, receiver.alreadyVerified(2, 2, vB, sig),
		"cache hit must NOT cross to a different value — the partial cannot sign V_b")
}
