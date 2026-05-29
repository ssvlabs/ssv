package validation

import (
	"testing"
	"time"

	"github.com/jellydator/ttlcache/v3"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft"
)

// Benchmark B6 (docs/OBFT-VALIDATION-VERIFIER-CACHE-PLAN.md): quantify the
// per-envelope cost the validation-layer Verifier cache removes.
//
// Both paths run the same herumi BLS pairing inside VerifyPhase1Bundle, so
// that ~0.9 ms is common overhead; the delta — especially in allocs/op —
// isolates what the cache unlocks: skipping NewVerifierFromShare's map
// builds + signer construction AND warming the F2 signing-root cache so the
// V → proposer-domain-root translation (SSZ-unmarshal + tree-root, ~336
// allocs on the B2 fixture) runs once per validator instead of per envelope.
//
//   - cold:   NewVerifierFromShare + VerifyPhase1Bundle  (status quo — fresh
//             cold Verifier per envelope)
//   - cached: obftVerifierFor (cache hit) + VerifyPhase1Bundle  (post-change —
//             reused Verifier, warm F2 cache)
//
// The Commit path would additionally show the F3 (kyber pubkey-parse) unlock
// on NR partials, but Phase1Bundle is the cleanest single-verify shape; the
// F3 win is already measured standalone by B3.
func BenchmarkOBFTVerifierCache_ColdVsCached(b *testing.B) {
	mv, ks, share, _, clusterID := obftTestSetup(b)
	signer := share.Committee[0].Signer

	// Build a BLS-valid Phase1Bundle once: real σ_V over a real blinded block,
	// signed with the production proposer-domain wiring.
	innerSigner := blsbackend.New(ks.Shares[signer].Serialize())
	vSigner, err := obftadapter.NewProposerSigner(innerSigner, mv.netCfg.Beacon)
	if err != nil {
		b.Fatalf("NewProposerSigner: %v", err)
	}
	v := proposerCandidateV()
	sigV, err := vSigner.SignPartial(v)
	if err != nil {
		b.Fatalf("SignPartial: %v", err)
	}
	bundle := &obftcore.Phase1Bundle{
		ClusterID:   clusterID,
		OperatorID:  obftcore.OperatorID(signer),
		Height:      obftTestHeight(mv),
		Layer:       0,
		Value:       v,
		LeaderSigma: sigV,
	}

	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			verifier, err := obftadapter.NewVerifierFromShare(&share.Share, nil, mv.netCfg.Beacon)
			if err != nil {
				b.Fatalf("NewVerifierFromShare: %v", err)
			}
			if err := verifier.VerifyPhase1Bundle(bundle); err != nil {
				b.Fatalf("VerifyPhase1Bundle: %v", err)
			}
		}
	})

	b.Run("cached", func(b *testing.B) {
		// Fresh cache; the first lookup builds + warms, the rest hit.
		mv.obftVerifiers = ttlcache.New(ttlcache.WithTTL[string, *cachedOBFTVerifier](time.Minute))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			verifier, err := mv.obftVerifierFor(share)
			if err != nil {
				b.Fatalf("obftVerifierFor: %v", err)
			}
			if err := verifier.VerifyPhase1Bundle(bundle); err != nil {
				b.Fatalf("VerifyPhase1Bundle: %v", err)
			}
		}
	})
}
