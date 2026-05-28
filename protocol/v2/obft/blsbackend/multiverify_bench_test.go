package blsbackend

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/ssvlabs/ssv/utils/threshold"
)

// Benchmark B4 from docs/OBFT-PERFORMANCE-AUDIT-PLAN.md: quantify the win
// from herumi's bls.MultiVerify vs sequential VerifyByte at realistic N's.
// Grounds F4 (batch-verify for NR partials and σ-walk loops).
//
// herumi's MultiVerify expects all messages concatenated as a single buffer
// of N*32 bytes; each msg slot is exactly 32 bytes. OBFT signs over signing
// roots / NR tags, both 32 bytes — exact fit.

// makeBenchVerifyTuples generates n (sig, pub, msg32) tuples from independent
// random shares — one tuple per "operator" in a hypothetical batch. Mirrors
// the shape of OBFT's σ-walk: n distinct shares, common or distinct msgs.
//
// `commonMsg=true` builds all tuples with the same msg (the σ-walk case at
// one layer for one V). `commonMsg=false` builds all distinct msgs (the
// NR-partial-loop case: same share, different tags per layer — but we can
// still benchmark with N distinct shares since the verify shape is identical).
func makeBenchVerifyTuples(b *testing.B, n int, commonMsg bool) (sigs []bls.Sign, pubs []bls.PublicKey, concatMsg []byte) {
	b.Helper()
	threshold.Init()

	sigs = make([]bls.Sign, n)
	pubs = make([]bls.PublicKey, n)
	concatMsg = make([]byte, n*32)

	var sharedMsg [32]byte
	if commonMsg {
		if _, err := rand.Read(sharedMsg[:]); err != nil {
			b.Fatalf("rand: %v", err)
		}
	}

	for i := 0; i < n; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		pubs[i] = *sk.GetPublicKey()

		var msg [32]byte
		if commonMsg {
			msg = sharedMsg
		} else {
			if _, err := rand.Read(msg[:]); err != nil {
				b.Fatalf("rand: %v", err)
			}
		}
		copy(concatMsg[i*32:(i+1)*32], msg[:])

		sig := sk.SignByte(msg[:])
		if sig == nil {
			b.Fatalf("SignByte returned nil")
		}
		sigs[i] = *sig
	}
	return sigs, pubs, concatMsg
}

func BenchmarkVerify_Sequential_CommonMsg(b *testing.B) {
	for _, n := range []int{3, 6, 13} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			sigs, pubs, concat := makeBenchVerifyTuples(b, n, true)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for j := 0; j < n; j++ {
					if !sigs[j].VerifyByte(&pubs[j], concat[j*32:(j+1)*32]) {
						b.Fatal("seq verify failed")
					}
				}
			}
		})
	}
}

func BenchmarkVerify_MultiVerify_CommonMsg(b *testing.B) {
	for _, n := range []int{3, 6, 13} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			sigs, pubs, concat := makeBenchVerifyTuples(b, n, true)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if !bls.MultiVerify(sigs, pubs, concat) {
					b.Fatal("MultiVerify failed")
				}
			}
		})
	}
}

func BenchmarkVerify_Sequential_DistinctMsgs(b *testing.B) {
	for _, n := range []int{3, 6, 13} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			sigs, pubs, concat := makeBenchVerifyTuples(b, n, false)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for j := 0; j < n; j++ {
					if !sigs[j].VerifyByte(&pubs[j], concat[j*32:(j+1)*32]) {
						b.Fatal("seq verify failed")
					}
				}
			}
		})
	}
}

func BenchmarkVerify_MultiVerify_DistinctMsgs(b *testing.B) {
	for _, n := range []int{3, 6, 13} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			sigs, pubs, concat := makeBenchVerifyTuples(b, n, false)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if !bls.MultiVerify(sigs, pubs, concat) {
					b.Fatal("MultiVerify failed")
				}
			}
		})
	}
}

// Sanity: confirm the bench fixture verifies both individually and as a batch.
// Catches accidental fixture corruption before reading the bench numbers.
//
// Skips under -race because bls.MultiVerify stores slice pointers in uintptr
// (eth.go:32-33, reconverted at eth.go:83) — a pattern Go's checkptr flags
// as invalid pointer arithmetic. Production builds run without checkptr and
// are unaffected. See docs/OBFT-F4-IMPLEMENTATION-PLAN.md §race-detector.
func TestMultiVerify_Fixture(t *testing.T) {
	skipIfRace(t)
	tCases := []struct {
		name      string
		commonMsg bool
	}{
		{"common-msg", true},
		{"distinct-msgs", false},
	}
	for _, tc := range tCases {
		t.Run(tc.name, func(t *testing.T) {
			threshold.Init()
			n := 6
			sigs := make([]bls.Sign, n)
			pubs := make([]bls.PublicKey, n)
			concat := make([]byte, n*32)
			var sharedMsg [32]byte
			if tc.commonMsg {
				if _, err := rand.Read(sharedMsg[:]); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < n; i++ {
				sk := &bls.SecretKey{}
				sk.SetByCSPRNG()
				pubs[i] = *sk.GetPublicKey()
				var msg [32]byte
				if tc.commonMsg {
					msg = sharedMsg
				} else {
					if _, err := rand.Read(msg[:]); err != nil {
						t.Fatal(err)
					}
				}
				copy(concat[i*32:(i+1)*32], msg[:])
				sig := sk.SignByte(msg[:])
				sigs[i] = *sig
			}
			// Sequential.
			for j := 0; j < n; j++ {
				if !sigs[j].VerifyByte(&pubs[j], concat[j*32:(j+1)*32]) {
					t.Fatalf("seq verify[%d] failed", j)
				}
			}
			// Batch.
			if !bls.MultiVerify(sigs, pubs, concat) {
				t.Fatal("MultiVerify failed")
			}
			// Bytes really differ vs all-same (paranoia against zero-init bugs).
			if tc.commonMsg {
				for j := 1; j < n; j++ {
					if !bytes.Equal(concat[:32], concat[j*32:(j+1)*32]) {
						t.Fatalf("common-msg fixture corrupted: tuple %d differs from tuple 0", j)
					}
				}
			}
		})
	}
}
