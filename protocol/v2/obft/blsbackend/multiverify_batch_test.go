package blsbackend

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// Tests for F4 — Signer.VerifyPartialBatch on the three concrete backends
// (BLSSigner / KyberSigner / StubSigner). See docs/OBFT-F4-IMPLEMENTATION-PLAN.md.
//
// The BLSSigner path goes through herumi's bls.MultiVerify, which stores
// slice pointers as uintptr (eth.go:32-33) reconverted at eth.go:83 — a
// pattern Go's -race checkptr considers invalid pointer arithmetic. Production
// builds don't enable checkptr and are unaffected; tests gate on skipIfRace.
//
// KyberSigner + StubSigner fall back to sequential VerifyPartial loops — they
// don't trigger the herumi issue and run normally under -race.

// skipIfRace skips the test when the binary is built with -race. herumi's
// MultiVerify trips checkptr's "pointer arithmetic result points to invalid
// allocation" check due to its uintptr storage pattern (eth.go:32-33,
// reconverted at eth.go:83). This is a known upstream pattern, not a real
// memory-safety issue: the underlying slice is still alive on the stack
// frame and the C call dereferences it correctly. Production builds don't
// use -race and are unaffected.
//
// raceEnabled is set via the build-tag pair multiverify_race_test.go (race)
// and multiverify_norace_test.go (!race), following the stdlib pattern.
//
// TODO(herumi#70): once https://github.com/herumi/bls-eth-go-binary/issues/70
// ships in an upstream release and ssvlabs/ssv bumps to it, this whole
// workaround can be removed. Cleanup steps:
//  1. Delete this skipIfRace helper.
//  2. Delete the build-tag pair multiverify_race_test.go and
//     multiverify_norace_test.go.
//  3. Remove the skipIfRace(t) call at the top of TestMultiVerify_Fixture in
//     multiverify_bench_test.go.
//  4. Remove the skipIfRace(t) call at the top of each
//     TestBLSSigner_VerifyPartialBatch_* test below.
//  5. Run `go test -race ./protocol/v2/obft/blsbackend/...` to confirm the
//     tests pass under -race without the skip.
//  6. Drop the §race-detector section from docs/OBFT-F4-IMPLEMENTATION-PLAN.md
//     (or mark it resolved).
func skipIfRace(t *testing.T) {
	t.Helper()
	if raceEnabled {
		t.Skip("skipped under -race: herumi/bls.MultiVerify trips Go's checkptr " +
			"due to uintptr storage of slice pointers in eth.go:32-33. " +
			"Production builds run without checkptr and are unaffected. " +
			"See docs/OBFT-F4-IMPLEMENTATION-PLAN.md §race-detector.")
	}
}

// makeValidBatch generates N valid (pub, msg32, sig) tuples from independent
// random shares — mirrors the σ-walk's "many ops, common message" shape but
// uses distinct messages so MultiVerify's "all msgs distinct" optimization
// path is exercised when present.
func makeValidBatch(t *testing.T, n int) (pubs [][]byte, msgs [][]byte, sigs []obft.Signature) {
	t.Helper()
	threshold.Init()
	pubs = make([][]byte, n)
	msgs = make([][]byte, n)
	sigs = make([]obft.Signature, n)
	for i := 0; i < n; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		pubs[i] = sk.GetPublicKey().Serialize()
		var msg [32]byte
		if _, err := rand.Read(msg[:]); err != nil {
			t.Fatalf("rand: %v", err)
		}
		msgs[i] = append([]byte(nil), msg[:]...)
		sig := sk.SignByte(msg[:])
		require.NotNil(t, sig)
		sigs[i] = obft.Signature(sig.Serialize())
	}
	return pubs, msgs, sigs
}

// TestBLSSigner_VerifyPartialBatch_AllValid — happy path: every tuple
// verifies, batch returns true. Exercises the herumi MultiVerify single-
// threaded branch (n < 16) and matches the σ-walk's realistic cluster sizes.
func TestBLSSigner_VerifyPartialBatch_AllValid(t *testing.T) {
	skipIfRace(t)
	signer := New(nil)
	for _, n := range []int{3, 6, 13} {
		pubs, msgs, sigs := makeValidBatch(t, n)
		require.True(t, signer.VerifyPartialBatch(pubs, msgs, sigs),
			"happy-path batch verify at n=%d must return true", n)
	}
}

// TestBLSSigner_VerifyPartialBatch_OneTampered — flipping bits in any one sig
// must fail the batch. Confirms MultiVerify's random-linear-combination
// rejection. The test does NOT assert which tuple failed (that's the σ-walk
// fallback's job); only that the batch returns false.
func TestBLSSigner_VerifyPartialBatch_OneTampered(t *testing.T) {
	skipIfRace(t)
	signer := New(nil)
	pubs, msgs, sigs := makeValidBatch(t, 6)
	tampered := append([]byte(nil), sigs[3]...)
	tampered[0] ^= 0xFF
	sigs[3] = tampered
	require.False(t, signer.VerifyPartialBatch(pubs, msgs, sigs),
		"one tampered sig must fail the whole batch")
}

// TestBLSSigner_VerifyPartialBatch_LengthMismatch — input-validation contract:
// any length disagreement among pubs/msgs/sigs returns false without invoking
// MultiVerify (which would panic on mismatched slice lengths).
func TestBLSSigner_VerifyPartialBatch_LengthMismatch(t *testing.T) {
	skipIfRace(t)
	signer := New(nil)
	pubs, msgs, sigs := makeValidBatch(t, 3)

	require.False(t, signer.VerifyPartialBatch(pubs[:2], msgs, sigs),
		"len(pubs)<len(sigs) must return false")
	require.False(t, signer.VerifyPartialBatch(pubs, msgs[:2], sigs),
		"len(msgs)<len(sigs) must return false")
	require.False(t, signer.VerifyPartialBatch(pubs, msgs, sigs[:2]),
		"len(sigs) shorter than the rest must return false")
}

// TestBLSSigner_VerifyPartialBatch_NonStandardMsgLen — every msg must be
// exactly 32 bytes (herumi MultiVerify's concatenated-buffer contract).
// A short or long msg fails the input validation before reaching herumi.
func TestBLSSigner_VerifyPartialBatch_NonStandardMsgLen(t *testing.T) {
	skipIfRace(t)
	signer := New(nil)
	pubs, msgs, sigs := makeValidBatch(t, 3)

	msgs[1] = msgs[1][:16] // 16 bytes, not 32
	require.False(t, signer.VerifyPartialBatch(pubs, msgs, sigs),
		"a non-32-byte msg must fail input validation")

	msgs[1] = append(append([]byte{}, msgs[1]...), bytes.Repeat([]byte{0}, 32)...) // 48 bytes
	require.False(t, signer.VerifyPartialBatch(pubs, msgs, sigs),
		"a 48-byte msg must fail input validation")
}

// TestBLSSigner_VerifyPartialBatch_EmptyBatch — N=0 returns false. The
// Signer contract is "ALL N tuples verify" with N ≥ 1; an empty batch has
// no positive truth to assert. Calling MultiVerify with an empty slice
// would also dereference &sigs[0] and panic.
func TestBLSSigner_VerifyPartialBatch_EmptyBatch(t *testing.T) {
	skipIfRace(t)
	signer := New(nil)
	require.False(t, signer.VerifyPartialBatch(nil, nil, nil),
		"empty batch must return false (contract: N ≥ 1)")
	require.False(t, signer.VerifyPartialBatch([][]byte{}, [][]byte{}, []obft.Signature{}),
		"zero-length non-nil slices must return false")
}

// TestBLSSigner_VerifyPartialBatch_MalformedPubOrSig — a deserialization
// failure on any pub or sig fails the batch (without panicking through into
// MultiVerify). Confirms each per-tuple Deserialize error path returns false.
func TestBLSSigner_VerifyPartialBatch_MalformedPubOrSig(t *testing.T) {
	skipIfRace(t)
	signer := New(nil)
	pubs, msgs, sigs := makeValidBatch(t, 3)

	pubsBad := [][]byte{pubs[0], []byte("not-a-pubkey"), pubs[2]}
	require.False(t, signer.VerifyPartialBatch(pubsBad, msgs, sigs),
		"a malformed pubkey must fail input validation")

	sigsBad := []obft.Signature{sigs[0], obft.Signature("not-a-sig"), sigs[2]}
	require.False(t, signer.VerifyPartialBatch(pubs, msgs, sigsBad),
		"a malformed sig must fail input validation")
}

// TestKyberSigner_VerifyPartialBatch_AllValid — sequential fallback: every
// tuple verifies, batch returns true. Each call goes through the F3 pub-cache
// so warm tuples don't re-parse the pubkey.
func TestKyberSigner_VerifyPartialBatch_AllValid(t *testing.T) {
	threshold.Init()
	verifier := NewKyberSigner(nil)
	const n = 6
	pubs := make([][]byte, n)
	msgs := make([][]byte, n)
	sigs := make([]obft.Signature, n)
	for i := 0; i < n; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		pubs[i] = sk.GetPublicKey().Serialize()
		var msg [32]byte
		if _, err := rand.Read(msg[:]); err != nil {
			t.Fatalf("rand: %v", err)
		}
		msgs[i] = append([]byte(nil), msg[:]...)
		opSigner := NewKyberSigner(sk.Serialize())
		sig, err := opSigner.SignPartial(msg[:])
		require.NoError(t, err)
		sigs[i] = sig
	}
	require.True(t, verifier.VerifyPartialBatch(pubs, msgs, sigs),
		"kyber sequential batch verify must succeed on valid tuples")
	require.Len(t, verifier.pubCache, n,
		"each pubkey must have populated the F3 cache during the batch")
}

// TestKyberSigner_VerifyPartialBatch_OneTampered — sequential short-circuits
// on the first bad sig. The remaining tuples are NOT verified; that's
// intentional (matches BLSSigner's batch semantics and is what the σ-walk
// fallback path will check against).
func TestKyberSigner_VerifyPartialBatch_OneTampered(t *testing.T) {
	threshold.Init()
	verifier := NewKyberSigner(nil)
	const n = 3
	pubs := make([][]byte, n)
	msgs := make([][]byte, n)
	sigs := make([]obft.Signature, n)
	for i := 0; i < n; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		pubs[i] = sk.GetPublicKey().Serialize()
		var msg [32]byte
		if _, err := rand.Read(msg[:]); err != nil {
			t.Fatalf("rand: %v", err)
		}
		msgs[i] = append([]byte(nil), msg[:]...)
		opSigner := NewKyberSigner(sk.Serialize())
		sig, err := opSigner.SignPartial(msg[:])
		require.NoError(t, err)
		sigs[i] = sig
	}
	tampered := append([]byte(nil), sigs[1]...)
	tampered[0] ^= 0xFF
	sigs[1] = tampered
	require.False(t, verifier.VerifyPartialBatch(pubs, msgs, sigs),
		"kyber sequential batch must fail on a tampered sig")
}

// TestKyberSigner_VerifyPartialBatch_NonStandardMsgLen — the kyber sequential
// fallback applies the same 32-byte msg-size guard as BLSSigner (interface
// contract uniformity).
func TestKyberSigner_VerifyPartialBatch_NonStandardMsgLen(t *testing.T) {
	threshold.Init()
	verifier := NewKyberSigner(nil)
	pubs := [][]byte{make([]byte, 48), make([]byte, 48), make([]byte, 48)}
	msgs := [][]byte{make([]byte, 32), make([]byte, 16), make([]byte, 32)} // [1] is 16 bytes
	sigs := []obft.Signature{make([]byte, 96), make([]byte, 96), make([]byte, 96)}
	require.False(t, verifier.VerifyPartialBatch(pubs, msgs, sigs),
		"non-32-byte msg must fail kyber batch input validation")
}

// TestStubSigner_VerifyPartialBatch_AllValid — sequential fallback parity
// for protocol-level tests. The stub's VerifyPartial is just byte-compare,
// so a happy batch is the same as N happy individual verifies.
func TestStubSigner_VerifyPartialBatch_AllValid(t *testing.T) {
	const n = 4
	pubs := make([][]byte, n)
	msgs := make([][]byte, n)
	sigs := make([]obft.Signature, n)
	for i := 0; i < n; i++ {
		share := []byte{byte(i + 1)}
		pubs[i] = share // stub uses share-bytes directly as pubKeyShare
		var msg [32]byte
		msg[0] = byte(i)
		msgs[i] = append([]byte(nil), msg[:]...)
		s := obft.NewStubSigner(3, share)
		sig, err := s.SignPartial(msg[:])
		require.NoError(t, err)
		sigs[i] = sig
	}
	verifier := obft.NewStubSigner(3, nil)
	require.True(t, verifier.VerifyPartialBatch(pubs, msgs, sigs))
}

// TestStubSigner_VerifyPartialBatch_OneTampered — stub returns false on the
// first failing verify. Matches the real backends' short-circuit semantics
// so protocol-level tests can rely on consistent batch behaviour.
func TestStubSigner_VerifyPartialBatch_OneTampered(t *testing.T) {
	const n = 4
	pubs := make([][]byte, n)
	msgs := make([][]byte, n)
	sigs := make([]obft.Signature, n)
	for i := 0; i < n; i++ {
		share := []byte{byte(i + 1)}
		pubs[i] = share
		var msg [32]byte
		msg[0] = byte(i)
		msgs[i] = append([]byte(nil), msg[:]...)
		s := obft.NewStubSigner(3, share)
		sig, err := s.SignPartial(msg[:])
		require.NoError(t, err)
		sigs[i] = sig
	}
	// Corrupt sigs[2] — last byte flip.
	tampered := append([]byte(nil), sigs[2]...)
	tampered[len(tampered)-1] ^= 0xFF
	sigs[2] = tampered

	verifier := obft.NewStubSigner(3, nil)
	require.False(t, verifier.VerifyPartialBatch(pubs, msgs, sigs))
}

// TestStubSigner_VerifyPartialBatch_LengthMismatch — same input-validation
// shape as the real backends.
func TestStubSigner_VerifyPartialBatch_LengthMismatch(t *testing.T) {
	verifier := obft.NewStubSigner(3, nil)
	require.False(t, verifier.VerifyPartialBatch(nil, nil, nil))
	require.False(t, verifier.VerifyPartialBatch([][]byte{nil}, [][]byte{nil, nil}, []obft.Signature{nil}))
}

