package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the F5 SkipNRPartialReverify gate (audit) and the F1 per-Instance
// verify-cache. See:
//   - docs/OBFT-PERFORMANCE-AUDIT-PLAN.md §F5 / §F1
//   - docs/OBFT-F1-F5-IMPLEMENTATION-PLAN.md (full safety contract)
//
// Twoab's NR-side verify is centralised in verifyNRTagPartial (called from
// 5 sites in phase2a / phase2b); F5 gates that helper. Twoab's σ-side
// Resolve verify lives at phase3.go's L_k>0 walk; F1 caches it. L_0 σ
// partials flow through sigmaPool (already verified at observation), so
// the cache doesn't help L_0 — but the helpers are exercised here directly
// for the safety properties.

// --------------------------- F5 ---------------------------

// TestTwoab_SkipNRPartialReverify_DefaultStillVerifies — with the default
// (zero) Config.SkipNRPartialReverify, a Commit carrying a malformed nr_tag_0
// L0Partial MUST be rejected by the in-Instance verify: addToNrTagPool is
// only called when verifyNRTagPartial returns true. We assert the malformed
// op never appears in nrTagPool[0].
func TestTwoab_SkipNRPartialReverify_DefaultStillVerifies(t *testing.T) {
	s := newSim(t, 4)
	require.False(t, s.cfg.SkipNRPartialReverify,
		"newSim must leave the flag at its safe default")
	receiver := s.instances[3]

	// Build op2's nr_tag_0 partial then tamper with it.
	signer := NewStubSigner(s.cfg.QV(), s.pubShares[2])
	tag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrSig, err := signer.SignPartial(tag)
	require.NoError(t, err)
	malformed := append([]byte{0xFF}, nrSig...)

	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  malformed,
	}
	require.NoError(t, receiver.ObserveCommit(c))

	// Default flag → verify fails → op2 is NOT pooled.
	pool := receiver.nrTagPool[0]
	_, pooled := pool[2]
	require.False(t, pooled,
		"default-false flag must reject malformed L0Partial — op MUST NOT enter nrTagPool")
}

// TestTwoab_SkipNRPartialReverify_TrueBypassesVerify — with the flag true,
// verifyNRTagPartial short-circuits and op IS pooled even on malformed bytes.
// In production this is safe because the validation-layer Verifier rejected
// the envelope upstream; the test just confirms the flag actually skips the
// verify.
func TestTwoab_SkipNRPartialReverify_TrueBypassesVerify(t *testing.T) {
	s := newSim(t, 4)
	skipCfg := *s.cfg
	skipCfg.SkipNRPartialReverify = true

	receiver, err := NewInstance(
		&skipCfg, 4,
		NewStubSigner(s.cfg.QV(), s.pubShares[4]),
		NewStubSigner(s.cfg.QV(), s.pubShares[4]),
		NewStubIBE(s.cfg.QEnc()),
		[]byte{0xCC, 0xDD},
		s.pubShares,
		nil, nil,
	)
	require.NoError(t, err)

	sender := NewStubSigner(s.cfg.QV(), s.pubShares[2])
	tag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrSig, err := sender.SignPartial(tag)
	require.NoError(t, err)
	malformed := append([]byte{0xFF}, nrSig...)

	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  malformed,
	}
	require.NoError(t, receiver.ObserveCommit(c))

	pool := receiver.nrTagPool[0]
	_, pooled := pool[2]
	require.True(t, pooled,
		"flag true must skip verify → malformed partial still pooled (production has upstream Verifier as backstop)")
}

// --------------------------- F1 ---------------------------

// TestTwoab_F1_VerifyOrCached_MissThenHit — direct test of verifyOrCached's
// miss-then-hit transition: first call runs the BLS verify and populates
// the cache; second call hits the cache and skips the BLS verify. This is
// the behaviour twoab's phase3.go L_k>0 σ-walk relies on for opportunistic
// re-Resolves to skip redundant work.
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
// populate the cache. Direct exercise of the safety invariant.
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

// TestTwoab_F1_EquivocationDistinctPartialsCachedIndependently — distinct
// partial-byte sequences at the same (op, layer, value) cache independently.
// The partialRoot in verifyCacheKey is the disambiguator.
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
// property: a cache populate for (op, layer, V_a, σ) MUST NOT make a lookup
// for (op, layer, V_b, σ) hit. Without valueRoot in the key, byzantine
// could leak σ that signs V_a into V_b's σ-pool. See verifyCacheKey doc.
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
