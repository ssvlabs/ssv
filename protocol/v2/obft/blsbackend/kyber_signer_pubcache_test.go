package blsbackend

import (
	"bytes"
	"sync"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/utils/threshold"
)

// Tests for the KyberSigner per-signer pub-key parse cache (audit finding F3).
//
// The cache amortises HerumiPubkeyToKyberG1Point — a ~100-300 µs G1
// decompression + subgroup check — across cluster-lifetime-stable pub-shares.
// Safety property: a cache HIT means the pubkey-bytes input was previously
// parsed successfully into the cached point; a different pubkey-bytes input
// keys a different cache entry. There is no cross-pubkey leakage. Concurrent
// verify calls (a single Verifier is shared across the message-validation
// pool) MUST be race-clean — exercised here under -race.

// TestKyberSigner_PubCache_PopulatesOnFirstVerify — the first VerifyPartial
// call with a pub-share that hasn't been seen before populates the cache;
// the next call with the same bytes finds it.
func TestKyberSigner_PubCache_PopulatesOnFirstVerify(t *testing.T) {
	threshold.Init()
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	pubBytes := master.GetPublicKey().Serialize()

	signer := NewKyberSigner(master.Serialize())
	msg := []byte("the-message-to-sign")
	sig, err := signer.SignPartial(msg)
	require.NoError(t, err)

	require.Empty(t, signer.pubCache, "cache must start empty")
	require.True(t, signer.VerifyPartial(pubBytes, msg, sig),
		"first VerifyPartial must succeed (round-trip)")
	require.Len(t, signer.pubCache, 1,
		"first VerifyPartial must populate exactly one cache entry")
	require.True(t, signer.VerifyPartial(pubBytes, msg, sig),
		"second VerifyPartial with same pub-share must hit the cache and succeed")
	require.Len(t, signer.pubCache, 1,
		"second VerifyPartial must NOT add a new cache entry")
}

// TestKyberSigner_PubCache_DistinctPubkeysCachedIndependently — two distinct
// operator pub-shares produce two distinct cache entries. There is no
// cross-pubkey leakage.
func TestKyberSigner_PubCache_DistinctPubkeysCachedIndependently(t *testing.T) {
	threshold.Init()
	a := &bls.SecretKey{}
	a.SetByCSPRNG()
	b := &bls.SecretKey{}
	b.SetByCSPRNG()

	signerA := NewKyberSigner(a.Serialize())
	signerB := NewKyberSigner(b.Serialize())

	msg := []byte("test")
	sigA, err := signerA.SignPartial(msg)
	require.NoError(t, err)
	sigB, err := signerB.SignPartial(msg)
	require.NoError(t, err)

	// Use a fresh KyberSigner as the verifier so both pub-shares are first-
	// time observations of its cache (we want to assert both entries populate
	// in the SAME signer).
	verifier := NewKyberSigner(nil)
	require.True(t, verifier.VerifyPartial(a.GetPublicKey().Serialize(), msg, sigA))
	require.True(t, verifier.VerifyPartial(b.GetPublicKey().Serialize(), msg, sigB))
	require.Len(t, verifier.pubCache, 2,
		"distinct pubkey bytes must produce distinct cache entries")
}

// TestKyberSigner_PubCache_FailedVerifyStillPopulates — a verify that fails
// because the SIGNATURE is wrong (but the pub-share itself is well-formed)
// still populates the cache for that pubkey. The cache is parse-result-
// bound, not verify-outcome-bound; storing a parsed point for a pubkey
// that's been seen is always safe (the point is the mathematical object,
// the verify outcome depends on the sig + msg).
func TestKyberSigner_PubCache_FailedVerifyStillPopulates(t *testing.T) {
	threshold.Init()
	a := &bls.SecretKey{}
	a.SetByCSPRNG()
	b := &bls.SecretKey{}
	b.SetByCSPRNG()

	signer := NewKyberSigner(b.Serialize()) // signing under b
	sig, err := signer.SignPartial([]byte("hi"))
	require.NoError(t, err)

	verifier := NewKyberSigner(nil)
	// Verify under a's pubkey (wrong pubkey for b's sig): MUST fail.
	require.False(t, verifier.VerifyPartial(a.GetPublicKey().Serialize(), []byte("hi"), sig))
	require.Len(t, verifier.pubCache, 1,
		"the pubkey parse succeeded → cache populates even though verify failed")
}

// TestKyberSigner_PubCache_MalformedPubkeyNoEntry — a pub-share that can't
// be parsed (HerumiPubkeyToKyberG1Point fails) MUST NOT add a cache entry.
// A miss-then-error path leaves the cache untouched.
func TestKyberSigner_PubCache_MalformedPubkeyNoEntry(t *testing.T) {
	verifier := NewKyberSigner(nil)
	malformed := []byte("not-a-valid-G1-point") // wrong length, fails the size guard

	require.False(t, verifier.VerifyPartial(malformed, []byte("msg"), []byte("sig")))
	require.Empty(t, verifier.pubCache,
		"malformed pubkey bytes must NOT populate the cache")
}

// TestKyberSigner_PubCache_ConcurrentVerify — concurrent verifies on the
// same signer (mirrors the production message-validation pool calling a
// single Verifier from multiple goroutines) MUST be race-clean. Also asserts
// the cache ends with one entry per distinct pubkey, regardless of how many
// concurrent callers raced to populate it.
func TestKyberSigner_PubCache_ConcurrentVerify(t *testing.T) {
	threshold.Init()
	signer := &bls.SecretKey{}
	signer.SetByCSPRNG()
	pub := signer.GetPublicKey().Serialize()

	signerWrap := NewKyberSigner(signer.Serialize())
	msg := []byte("concurrent")
	sig, err := signerWrap.SignPartial(msg)
	require.NoError(t, err)

	verifier := NewKyberSigner(nil)
	const goroutines = 16
	const calls = 64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < calls; i++ {
				if !verifier.VerifyPartial(pub, msg, sig) {
					t.Errorf("VerifyPartial failed under concurrency")
					return
				}
			}
		}()
	}
	wg.Wait()
	require.Len(t, verifier.pubCache, 1,
		"concurrent populates for the same pubkey must converge to one entry")
}

// TestKyberSigner_PubCache_CachedPointMatchesFreshParse — sanity: the cached
// point is mathematically equivalent to a fresh parse via the same conversion.
// Catches any future regression that accidentally stores a different point
// under the same key.
func TestKyberSigner_PubCache_CachedPointMatchesFreshParse(t *testing.T) {
	threshold.Init()
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()
	pubBytes := sk.GetPublicKey().Serialize()

	verifier := NewKyberSigner(nil)
	cached, err := verifier.cachedPubkeyPoint(pubBytes)
	require.NoError(t, err)
	fresh, err := HerumiPubkeyToKyberG1Point(pubBytes)
	require.NoError(t, err)

	cachedBytes, err := cached.MarshalBinary()
	require.NoError(t, err)
	freshBytes, err := fresh.MarshalBinary()
	require.NoError(t, err)
	require.True(t, bytes.Equal(cachedBytes, freshBytes),
		"cached point must serialise to the same bytes as a fresh parse")
}
