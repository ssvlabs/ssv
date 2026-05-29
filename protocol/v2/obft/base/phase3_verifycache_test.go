package base

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the per-Instance verify-cache used to skip redundant
// signer.VerifyPartial calls in Resolve (audit finding F1). The safety
// invariant in short form is "cache populate is gated EXCLUSIVELY by signer.VerifyPartial
// just returning true on that exact (op, layer, value, partial-bytes)
// tuple, and value-binding means a cache hit can never let a partial
// contribute to a V it doesn't sign".
//
// These tests directly exercise the cache helpers + each populate site
// (phase1 retention, phase2 peerSigmaAtL0Verdict, phase2 harvestWitness).
// The L_k>0 populate-on-first-success path in phase3.go is exercised by
// the verifyOrCached miss-then-hit test below; end-to-end correctness is
// covered by the existing TestObft_Healthy_n4 / TestObft_Multi* / consensustest
// stress suites which all pass with the F1 cache in place.

// TestObft_F1_Phase1BundleRetentionPopulatesCache — a successful
// ObservePhase1Bundle on a valid bundle MUST populate the cache for
// (leaderOp, layer, b.Value, leaderSigma). Confirms F1's first populate
// site (phase1.go around the existing signer.VerifyPartial call).
func TestObft_F1_Phase1BundleRetentionPopulatesCache(t *testing.T) {
	s := newSim(t, 4)
	v0 := s.candidates[0]
	bundle, err := s.instances[1].BuildPhase1Bundle(0, v0) // op1 is L_0 leader
	require.NoError(t, err)

	receiver := s.instances[2]
	require.False(t, receiver.alreadyVerified(1, 0, bundle.Value, bundle.LeaderSigma),
		"cache must be empty before observation")
	require.NoError(t, receiver.ObservePhase1Bundle(bundle, observedEarly))
	require.True(t, receiver.alreadyVerified(1, 0, bundle.Value, bundle.LeaderSigma),
		"successful ObservePhase1Bundle must populate the F1 cache")
}

// TestObft_F1_PeerL0OnionVerifyPopulatesCache — a peer's L_0 σ-onion entry
// that passes peerSigmaAtL0Verdict MUST populate the cache. Confirms F1's
// second populate site (phase2.go peerSigmaAtL0Verdict).
func TestObft_F1_PeerL0OnionVerifyPopulatesCache(t *testing.T) {
	s := newSim(t, 4)
	v0 := s.candidates[0]
	// peerSigmaAtL0Verdict needs the V retained locally to return
	// l0SigmaVerified (otherwise unknownV and the populate doesn't fire).
	s.deliverPhase1(0, v0, s.allOperators(), observedEarly, true)

	// Build op2's σ on v0 directly (matches what their KindCommit's L_0
	// onion entry would carry — plaintext at L_0).
	signer := NewStubSigner(s.cfg.QV(), []byte{2})
	sig, err := signer.SignPartial(v0)
	require.NoError(t, err)

	receiver := s.instances[3]
	require.False(t, receiver.alreadyVerified(2, 0, v0, sig),
		"cache must be empty before verdict")
	verdict := receiver.peerSigmaAtL0Verdict(2, EncryptedLayer{Value: v0, Ciphertext: sig})
	require.Equal(t, l0SigmaVerified, verdict, "fixture should verify under op2's share")
	require.True(t, receiver.alreadyVerified(2, 0, v0, sig),
		"l0SigmaVerified verdict must populate the F1 cache")
}

// TestObft_F1_WitnessHarvestPopulatesCache — a successful witness verify
// inside harvestWitness MUST populate the cache. Today Resolve doesn't
// re-verify witnesses but the populate is defensive future-proofing; see
// the inline comment at the populate site.
//
// Setup must bypass the Phase-1-bundle path because retaining a bundle
// pre-populates the same cache entry the witness path would (deterministic
// StubSigner produces identical σ bytes for the same share+msg), making
// the "cache empty pre-harvest" precondition impossible to assert.
// Manually seeding peerOnions for V (the source findVByRoot uses to resolve
// w.ValueRoot → v) sidesteps the bundle path.
func TestObft_F1_WitnessHarvestPopulatesCache(t *testing.T) {
	s := newSim(t, 4)
	// op2 is the L_1 leader. Use the L_1 candidate so leaders / layers
	// don't overlap with any L_0 setup the sim might have done implicitly.
	v1 := s.candidates[1]
	receiver := s.instances[3]

	// Seed peerOnions[1] with a non-leader entry carrying V=v1 so
	// findVByRoot(1, ValueRoot(v1)) returns v1. The Ciphertext bytes are
	// don't-care for findVByRoot; harvestWitness ignores them.
	receiver.peerOnions[1] = map[OperatorID][]EncryptedLayer{
		3: {{Value: v1, Ciphertext: []byte{0x01}}},
	}

	leaderSigner := NewStubSigner(s.cfg.QV(), []byte{2})
	leaderSigma, err := leaderSigner.SignPartial(v1)
	require.NoError(t, err)

	require.False(t, receiver.alreadyVerified(2, 1, v1, leaderSigma),
		"cache must be empty pre-harvest")
	receiver.harvestWitness(LeaderSigmaWitness{
		Layer:     1,
		Leader:    2,
		ValueRoot: ValueRoot(v1),
		Sigma:     leaderSigma,
	})
	require.True(t, receiver.alreadyVerified(2, 1, v1, leaderSigma),
		"successful witness harvest must populate the F1 cache")
}

// TestObft_F1_FailedVerifyNotCached — a malformed σ partial that fails BLS
// verify at peerSigmaAtL0Verdict MUST NOT populate the cache. The safety
// invariant requires "cache populate is gated EXCLUSIVELY by a SUCCESSFUL
// signer.VerifyPartial call"; without this, a malformed partial could later
// pass through Resolve via a phantom cache hit on the same byte sequence.
func TestObft_F1_FailedVerifyNotCached(t *testing.T) {
	s := newSim(t, 4)
	v0 := s.candidates[0]

	// Construct a malformed σ partial — prepend a byte so StubSigner's
	// byte-equality verify returns false.
	signer := NewStubSigner(s.cfg.QV(), []byte{2})
	realSig, err := signer.SignPartial(v0)
	require.NoError(t, err)
	malformed := append([]byte{0xFF}, realSig...)

	receiver := s.instances[3]
	verdict := receiver.peerSigmaAtL0Verdict(2, EncryptedLayer{Value: v0, Ciphertext: malformed})
	require.Equal(t, l0SigmaCryptoFake, verdict, "malformed sig must fail verify")
	require.False(t, receiver.alreadyVerified(2, 0, v0, malformed),
		"failed BLS verify MUST NOT populate the cache (safety invariant)")
}

// TestObft_F1_EquivocationDistinctPartialsCachedIndependently — a byzantine
// emitting two distinct partial-byte sequences at the same (op, layer, value)
// MUST cache them independently. A cache hit on one partial must NOT let a
// different partial bypass verify. partialRoot in verifyCacheKey is the
// load-bearing disambiguator for this byzantine case.
func TestObft_F1_EquivocationDistinctPartialsCachedIndependently(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]

	v := []byte("V_for_op2_at_L_0")
	sigA := []byte{0x10, 0x01, 0x02, 0x03}
	sigB := []byte{0x10, 0x99, 0x98, 0x97}

	receiver.markVerified(2, 0, v, sigA)
	require.True(t, receiver.alreadyVerified(2, 0, v, sigA), "A must be cached")
	require.False(t, receiver.alreadyVerified(2, 0, v, sigB),
		"distinct partial B must NOT inherit A's cache entry")

	receiver.markVerified(2, 0, v, sigB)
	require.True(t, receiver.alreadyVerified(2, 0, v, sigB), "B must now be cached")
	require.True(t, receiver.alreadyVerified(2, 0, v, sigA),
		"A must still be cached after caching B")
}

// TestObft_F1_ValueBoundCacheKey_NoCrossVLeak — the load-bearing safety
// property that motivates including valueRoot in the cache key.
//
// Attack the cache key WITHOUT valueRoot would admit: byzantine emits two
// L_k>0 onion entries from the same (op, layer) — entry A claims V_a and
// decrypts to σ_a (the leader's real σ on V_a), entry B claims V_b but
// decrypts to the same σ_a bytes. Resolve walks entry A first → verify σ_a
// against V_a passes → cache populates. Walk entry B → cache hit on
// (op, layer, sha256(σ_a)) → skip verify → addToGroup(V_b, op, σ_a) admits
// σ_a to V_b's pool incorrectly (σ_a doesn't sign V_b).
//
// valueRoot in the cache key blocks this: entry A's populate key is
// (op, layer, sha256(V_a), sha256(σ_a)); entry B's lookup key is
// (op, layer, sha256(V_b), sha256(σ_a)). Different valueRoot → cache miss
// → full verify → σ_a against V_b → fails → no contribution.
//
// This test exercises the helpers directly (rather than threading through
// an L_k>0 Resolve walk) because the property is at the cache-key level —
// the same property holds wherever the cache is consulted.
func TestObft_F1_ValueBoundCacheKey_NoCrossVLeak(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]

	vA := []byte("V_a")
	vB := []byte("V_b")
	sig := []byte("partial_sig_bytes_for_sigma_a")

	// Simulate the populate that would happen after a successful verify of
	// (V_a, sig) at, say, (op=2, layer=2).
	receiver.markVerified(2, 2, vA, sig)

	require.True(t, receiver.alreadyVerified(2, 2, vA, sig),
		"populate must produce a cache hit for the SAME (value, partial)")
	require.False(t, receiver.alreadyVerified(2, 2, vB, sig),
		"cache hit must NOT cross to a different value — the partial cannot sign vB")
}

// TestObft_F1_VerifyOrCached_MissThenHit — direct test of verifyOrCached's
// miss-then-hit transition: first call runs the BLS verify and populates
// the cache; second call hits the cache and skips the BLS verify. This is
// the behaviour Resolve relies on to skip redundant re-verifies at L_k > 0.
//
// Also checks the negative-cache safety property: a tampered partial-byte
// sequence at the same (op, layer, value) must NOT pass via cache hit on
// the real partial — it falls through to a fresh verify and gets rejected.
func TestObft_F1_VerifyOrCached_MissThenHit(t *testing.T) {
	s := newSim(t, 4)
	v0 := s.candidates[0]
	receiver := s.instances[3]
	pubShare := receiver.pubKeyShares[2]

	signer := NewStubSigner(s.cfg.QV(), []byte{2})
	partial, err := signer.SignPartial(v0)
	require.NoError(t, err)

	// First call: cache miss → BLS verify → populate cache.
	require.False(t, receiver.alreadyVerified(2, 0, v0, partial), "cache empty pre-call")
	require.True(t, receiver.verifyOrCached(2, 0, pubShare, v0, partial),
		"first call should verify and return true")
	require.True(t, receiver.alreadyVerified(2, 0, v0, partial),
		"verifyOrCached must populate the cache on first success")

	// Second call: cache hit → skip BLS verify → return true.
	require.True(t, receiver.verifyOrCached(2, 0, pubShare, v0, partial),
		"second call should hit the cache and still return true")

	// Tampered partial at the same (op, layer, value): must fall through
	// to a fresh BLS verify (which fails) and not populate the cache.
	tampered := append([]byte{0xFF}, partial...)
	require.False(t, receiver.verifyOrCached(2, 0, pubShare, v0, tampered),
		"tampered partial must fail full verify (cache miss → BLS verify → false)")
	require.False(t, receiver.alreadyVerified(2, 0, v0, tampered),
		"failed verify must not populate the cache for the tampered bytes")
}
