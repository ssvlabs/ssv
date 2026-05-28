package base

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the per-Instance verify-cache used to skip redundant
// signer.VerifyPartial calls in Resolve (audit finding F1). See
// docs/OBFT-F1-F5-IMPLEMENTATION-PLAN.md for the safety invariant; the
// short form is "cache populate is gated EXCLUSIVELY by signer.VerifyPartial
// just returning true on that exact (op, layer, partial-bytes) tuple".
//
// These tests directly exercise the cache helpers + each populate site
// (phase1 retention, phase2 peerSigmaAtL0Verdict). The L_k>0 populate-on-
// first-success path in phase3.go is exercised indirectly via the equivocation
// + verifyOrCached test below, and end-to-end correctness is covered by the
// existing TestObft_Healthy_n4 / TestObft_Multi* tests which all pass with
// the F1 cache in place.

// TestObft_F1_Phase1BundleRetentionPopulatesCache — a successful
// ObservePhase1Bundle on a valid bundle MUST populate the cache for
// (leaderOp, layer, leaderSigma). Confirms F1's first populate site
// (phase1.go around the existing signer.VerifyPartial call).
func TestObft_F1_Phase1BundleRetentionPopulatesCache(t *testing.T) {
	s := newSim(t, 4)
	v0 := s.candidates[0]
	bundle, err := s.instances[1].BuildPhase1Bundle(0, v0) // op1 is L_0 leader
	require.NoError(t, err)

	receiver := s.instances[2]
	require.False(t, receiver.alreadyVerified(1, 0, bundle.LeaderSigma),
		"cache must be empty before observation")
	require.NoError(t, receiver.ObservePhase1Bundle(bundle, observedEarly))
	require.True(t, receiver.alreadyVerified(1, 0, bundle.LeaderSigma),
		"successful ObservePhase1Bundle must populate the F1 cache")
}

// TestObft_F1_PeerL0OnionVerifyPopulatesCache — a peer's L_0 σ-onion entry
// that passes peerSigmaAtL0Verdict MUST populate the cache. Confirms F1's
// second populate site (phase2.go peerSigmaAtL0Verdict).
func TestObft_F1_PeerL0OnionVerifyPopulatesCache(t *testing.T) {
	s := newSim(t, 4)
	v0 := s.candidates[0]
	// peerSigmaAtL0Verdict needs the V retained locally to return l0SigmaVerified
	// (otherwise it returns unknownV and the cache populate doesn't fire).
	s.deliverPhase1(0, v0, s.allOperators(), observedEarly, true)

	// Build op2's σ on v0 directly (matches what their KindCommit's L_0
	// onion entry would carry — plaintext at L_0).
	signer := NewStubSigner(s.cfg.QV(), []byte{2})
	sig, err := signer.SignPartial(v0)
	require.NoError(t, err)

	receiver := s.instances[3]
	require.False(t, receiver.alreadyVerified(2, 0, sig),
		"cache must be empty before verdict")
	verdict := receiver.peerSigmaAtL0Verdict(2, EncryptedLayer{Value: v0, Ciphertext: sig})
	require.Equal(t, l0SigmaVerified, verdict, "fixture should verify under op2's share")
	require.True(t, receiver.alreadyVerified(2, 0, sig),
		"l0SigmaVerified verdict must populate the F1 cache")
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
	require.False(t, receiver.alreadyVerified(2, 0, malformed),
		"failed BLS verify MUST NOT populate the cache (safety invariant)")
}

// TestObft_F1_EquivocationDistinctPartialsCachedIndependently — a byzantine
// emitting two distinct partial-byte sequences at the same (op, layer) MUST
// cache them independently. A cache hit on one partial must NOT let a
// different partial bypass verify. partialRoot = sha256(partial-bytes) is
// the load-bearing disambiguator.
func TestObft_F1_EquivocationDistinctPartialsCachedIndependently(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]

	// Two distinct partial byte-sequences at the same (op=2, layer=0).
	sigA := []byte{0x10, 0x01, 0x02, 0x03}
	sigB := []byte{0x10, 0x99, 0x98, 0x97}

	receiver.markVerified(2, 0, sigA)
	require.True(t, receiver.alreadyVerified(2, 0, sigA), "A must be cached")
	require.False(t, receiver.alreadyVerified(2, 0, sigB),
		"distinct partial B must NOT inherit A's cache entry")

	receiver.markVerified(2, 0, sigB)
	require.True(t, receiver.alreadyVerified(2, 0, sigB), "B must now be cached")
	require.True(t, receiver.alreadyVerified(2, 0, sigA),
		"A must still be cached after caching B")
}

// TestObft_F1_VerifyOrCached_MissThenHit — direct test of verifyOrCached's
// miss-then-hit transition: first call runs the BLS verify and populates the
// cache; second call hits the cache and skips the BLS verify. This is the
// behaviour that Resolve relies on to skip redundant re-verifies at L_k > 0.
//
// Also checks the negative-cache safety property: a tampered partial-byte
// sequence at the same (op, layer) must NOT pass via the cache hit on the
// real partial — it falls through to a fresh verify and gets rejected.
func TestObft_F1_VerifyOrCached_MissThenHit(t *testing.T) {
	s := newSim(t, 4)
	v0 := s.candidates[0]
	receiver := s.instances[3]
	pubShare := receiver.pubKeyShares[2]

	signer := NewStubSigner(s.cfg.QV(), []byte{2})
	partial, err := signer.SignPartial(v0)
	require.NoError(t, err)

	// First call: cache miss → BLS verify → populate cache.
	require.False(t, receiver.alreadyVerified(2, 0, partial), "cache empty pre-call")
	require.True(t, receiver.verifyOrCached(2, 0, pubShare, v0, partial),
		"first call should verify and return true")
	require.True(t, receiver.alreadyVerified(2, 0, partial),
		"verifyOrCached must populate the cache on first success")

	// Second call: cache hit → skip BLS verify → return true.
	require.True(t, receiver.verifyOrCached(2, 0, pubShare, v0, partial),
		"second call should hit the cache and still return true")

	// Tampered partial at the same (op, layer): must fall through to a
	// fresh BLS verify (which fails) and not populate the cache.
	tampered := append([]byte{0xFF}, partial...)
	require.False(t, receiver.verifyOrCached(2, 0, pubShare, v0, tampered),
		"tampered partial must fail full verify (cache miss → BLS verify → false)")
	require.False(t, receiver.alreadyVerified(2, 0, tampered),
		"failed verify must not populate the cache for the tampered bytes")
}
