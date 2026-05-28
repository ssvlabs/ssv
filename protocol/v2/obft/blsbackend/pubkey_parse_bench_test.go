package blsbackend

import (
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
)

// Benchmark B3 from docs/OBFT-PERFORMANCE-AUDIT-PLAN.md: quantify the pubkey
// re-parse cost. Grounds F3 (KyberSigner.VerifyPartial re-parses pubkey)
// and F6 (BLSSigner.SignPartial re-deserializes share).
//
// Per-call cost is fixed (the pubkey/share bytes are stable), so the win
// from caching is the entire per-call cost minus a cheap map lookup.

// makeBenchPubkey produces a herumi-format compressed G1 pubkey (48 bytes)
// from a freshly-generated random share. Used by both pubkey-parse benches.
func makeBenchPubkey(b *testing.B) []byte {
	b.Helper()
	ensureInit()
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	return master.GetPublicKey().Serialize()
}

func BenchmarkHerumiPubkeyToKyberG1Point(b *testing.B) {
	pubBytes := makeBenchPubkey(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := HerumiPubkeyToKyberG1Point(pubBytes)
		if err != nil {
			b.Fatalf("HerumiPubkeyToKyberG1Point: %v", err)
		}
	}
}

func BenchmarkBLSPublicKey_Deserialize(b *testing.B) {
	pubBytes := makeBenchPubkey(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var pk bls.PublicKey
		if err := pk.Deserialize(pubBytes); err != nil {
			b.Fatalf("PublicKey.Deserialize: %v", err)
		}
	}
}

// makeBenchShare produces a herumi-format compressed G1 secret share
// (32 bytes) from threshold-split keys. Used by share-parse benches.
func makeBenchShare(b *testing.B) []byte {
	shareBytes, _ := makeBenchKeyset(b, 7)
	return shareBytes
}

func BenchmarkBLSSecretKey_Deserialize(b *testing.B) {
	shareBytes := makeBenchShare(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var sk bls.SecretKey
		if err := sk.Deserialize(shareBytes); err != nil {
			b.Fatalf("SecretKey.Deserialize: %v", err)
		}
	}
}

func BenchmarkHerumiShareToKyberScalar(b *testing.B) {
	shareBytes := makeBenchShare(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := HerumiShareToKyberScalar(shareBytes)
		if err != nil {
			b.Fatalf("HerumiShareToKyberScalar: %v", err)
		}
	}
}
