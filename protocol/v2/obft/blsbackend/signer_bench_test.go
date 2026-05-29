package blsbackend

import (
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/ssvlabs/ssv/utils/threshold"
)

// Benchmark B1: baseline BLS partial
// verify cost. Grounds the "~1 ms per verify" assumption underpinning F1, F3,
// F4, F5. Two variants — herumi (BLSSigner) and kyber (KyberSigner) — since
// the codebase uses both and the latter is noticeably slower per verify.

// makeBenchKeyset builds a fresh n-of-q threshold keypair and returns
// operator 1's share + pub-share bytes (herumi serialised). Re-used across
// the partial-verify and pubkey-parse benchmarks.
func makeBenchKeyset(b *testing.B, n int) (shareBytes, pubShareBytes []byte) {
	b.Helper()
	threshold.Init()
	f := (n - 1) / 3
	q := uint64(2*f + 1)

	master := &bls.SecretKey{}
	master.SetByCSPRNG()

	shares, err := threshold.Create(master.Serialize(), q, uint64(n))
	if err != nil {
		b.Fatalf("threshold.Create: %v", err)
	}
	sk := shares[1]
	return sk.Serialize(), sk.GetPublicKey().Serialize()
}

func BenchmarkBLSSigner_VerifyPartial(b *testing.B) {
	shareBytes, pubShareBytes := makeBenchKeyset(b, 7)
	signer := New(shareBytes)

	// 32-byte msg — matches OBFT's signing-root / NR-tag size.
	msg := make([]byte, 32)
	sig, err := signer.SignPartial(msg)
	if err != nil {
		b.Fatalf("SignPartial: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !signer.VerifyPartial(pubShareBytes, msg, sig) {
			b.Fatal("verify failed")
		}
	}
}

func BenchmarkKyberSigner_VerifyPartial(b *testing.B) {
	shareBytes, pubShareBytes := makeBenchKeyset(b, 7)
	signer := NewKyberSigner(shareBytes)

	// 32-byte msg matching the BLS variant — KyberSigner uses drand's NUL
	// DST internally, so the partial bytes are different even on the same
	// share + msg, but verify cost is what we measure here.
	msg := make([]byte, 32)
	sig, err := signer.SignPartial(msg)
	if err != nil {
		b.Fatalf("SignPartial: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !signer.VerifyPartial(pubShareBytes, msg, sig) {
			b.Fatal("verify failed")
		}
	}
}

// BenchmarkBLSSigner_SignPartial measures the per-call cost of the partial
// signing path, including the per-call SecretKey.Deserialize that F6 flags
// for amortization.
func BenchmarkBLSSigner_SignPartial(b *testing.B) {
	shareBytes, _ := makeBenchKeyset(b, 7)
	signer := New(shareBytes)

	msg := make([]byte, 32)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := signer.SignPartial(msg)
		if err != nil {
			b.Fatalf("SignPartial: %v", err)
		}
	}
}

// Sanity: confirm a 32-byte msg actually round-trips through both signers
// before the benchmark suite assumes it does. Failure here means the bench
// numbers are measuring an error path, not the real verify cost.
//
// Lives alongside the benchmarks because it's bench-fixture validation,
// not a behavior test of the signers themselves (those live in signer_test.go
// and kyber_signer_test.go).
func TestBenchFixture_VerifyRoundTrips(t *testing.T) {
	threshold.Init()
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	shares, err := threshold.Create(master.Serialize(), 5, 7)
	if err != nil {
		t.Fatalf("threshold.Create: %v", err)
	}
	sk := shares[1]
	shareBytes := sk.Serialize()
	pubShareBytes := sk.GetPublicKey().Serialize()
	msg := make([]byte, 32)

	bsigner := New(shareBytes)
	bsig, err := bsigner.SignPartial(msg)
	if err != nil {
		t.Fatalf("BLSSigner.SignPartial: %v", err)
	}
	if !bsigner.VerifyPartial(pubShareBytes, msg, bsig) {
		t.Fatal("BLSSigner round-trip verify failed")
	}

	ksigner := NewKyberSigner(shareBytes)
	ksig, err := ksigner.SignPartial(msg)
	if err != nil {
		t.Fatalf("KyberSigner.SignPartial: %v", err)
	}
	if !ksigner.VerifyPartial(pubShareBytes, msg, ksig) {
		t.Fatal("KyberSigner round-trip verify failed")
	}
}
