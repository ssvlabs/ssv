// Package blsbackend provides concrete OBFT primitive implementations
// backed by herumi/bls-eth-go-binary — the same BLS library SSV's existing
// validator-share infrastructure uses.
//
// This package exists to bridge the abstract interfaces in protocol/v2/obft
// (which know nothing about specific BLS libraries) with the concrete
// cryptography SSV operates with. It's not part of the OBFT core package
// because the core stays library-agnostic for testability and clarity.
//
// Under the chosen Option A (reuse the validator's existing threshold BLS
// key as the IBE trust anchor), this package's Signer is the production
// signer used both for value-signing (partial sigs that go inside Onions)
// and for tag-signing (partial sigs that aggregate into IBE decryption
// keys). The IBE primitive (tlock-backed) lives alongside in this package
// once integrated.
package blsbackend

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// initOnce ensures bls.Init is called exactly once per process. The herumi
// library is C-backed and re-initialization is wasteful (and may not be
// safe across all versions).
var initOnce sync.Once

func ensureInit() {
	initOnce.Do(func() {
		// Errors are intentionally ignored; matching SSV's existing
		// threshold.Init() pattern. Calling Init twice should be a no-op
		// in practice, but sync.Once is the safer guard.
		_ = bls.Init(bls.BLS12_381)
		_ = bls.SetETHmode(bls.EthModeDraft07)
	})
}

// BLSSigner is the herumi/bls-backed implementation of obft.Signer. A
// BLSSigner is bound to one operator's secret share at construction. The
// share material never leaves this struct via the public API (SignPartial
// takes only a message; the bound share is used internally).
//
// Aggregation and verification are stateless wrt the bound share — they
// can be called on any BLSSigner instance regardless of which share it
// was constructed for. In production the local operator's BLSSigner does
// double duty: signing for that operator, plus aggregating / verifying
// across the cluster.
//
// All inputs and outputs are serialized BLS bytes:
//
//   - share        : serialized bls.SecretKey bytes (operator's secret share)
//   - pubKeyShare  : serialized bls.PublicKey bytes (operator's pubkey share)
//   - clusterPubKey: serialized bls.PublicKey bytes (the master / validator pubkey)
//   - msg          : arbitrary bytes (signed via bls.SecretKey.SignByte / bls.Sign.VerifyByte)
//   - partial / sig: serialized bls.Sign bytes
type BLSSigner struct {
	// share is the operator's serialized BLS secret share. May be empty
	// for verify-only / aggregate-only use cases (SignPartial then
	// returns an error).
	share []byte
}

// New creates a BLSSigner bound to `share`. Calls bls.Init once per
// process. `share` may be nil/empty for verify-only / aggregate-only use
// cases — SignPartial then returns an error.
func New(share []byte) *BLSSigner {
	ensureInit()
	out := &BLSSigner{}
	if len(share) > 0 {
		out.share = append([]byte(nil), share...)
	}
	return out
}

// SignPartial signs `msg` with the operator's share that this signer was
// constructed against, and returns a serialized partial signature. Each
// invocation deserialises the share fresh; if the SSV adapter wants to
// amortize, it can keep a parsed key alongside and call the herumi API
// directly. Per-call deserialisation keeps this package trivially safe to
// use from multiple goroutines without sharing state.
func (s *BLSSigner) SignPartial(msg []byte) (obft.Signature, error) {
	if len(s.share) == 0 {
		return nil, fmt.Errorf("blsbackend: signer has no share bound (verify-only)")
	}
	if len(msg) == 0 {
		return nil, fmt.Errorf("blsbackend: empty message")
	}
	sk := &bls.SecretKey{}
	if err := sk.Deserialize(s.share); err != nil {
		return nil, fmt.Errorf("blsbackend: deserialize share: %w", err)
	}
	sig := sk.SignByte(msg)
	if sig == nil {
		return nil, fmt.Errorf("blsbackend: SignByte returned nil")
	}
	return obft.Signature(sig.Serialize()), nil
}

// AggregatePartials reconstructs a full BLS signature from partial signatures
// keyed by operator ID, using Lagrange interpolation. Any subset of size ≥ q
// (where q is the threshold the keys were generated for) yields the SAME
// reconstructed signature, regardless of which subset is used — this is the
// critical property OBFT relies on for cluster-wide consistency.
//
// The map keys are operator IDs; herumi's Recover uses these as the Shamir
// x-coordinates (encoded as bls.ID values). They must match the IDs used
// when the secret was split.
func (s *BLSSigner) AggregatePartials(partials map[obft.OperatorID]obft.Signature) (obft.Signature, error) {
	if len(partials) == 0 {
		return nil, fmt.Errorf("blsbackend: no partials to aggregate")
	}

	sigVec := make([]bls.Sign, 0, len(partials))
	idVec := make([]bls.ID, 0, len(partials))

	for opID, p := range partials {
		sig := bls.Sign{}
		if err := sig.Deserialize(p); err != nil {
			return nil, fmt.Errorf("blsbackend: deserialize partial for op %d: %w", opID, err)
		}
		sigVec = append(sigVec, sig)

		var blsID bls.ID
		// SSV's share infrastructure uses operator IDs directly as the Shamir
		// x-coordinates. SetLittleEndian sets the bls.ID (an Fr element) from
		// the little-endian bytes of opID — equivalent to the prior
		// SetDecString(fmt.Sprintf("%d", opID)) round-trip for these small
		// integer IDs, but without the per-call string allocation + decimal
		// parse (audit F18). Equivalence with the old decimal-string encoding
		// + an end-to-end reconstruction round-trip are pinned in
		// aggregate_id_test.go.
		var idBuf [8]byte
		binary.LittleEndian.PutUint64(idBuf[:], uint64(opID))
		if err := blsID.SetLittleEndian(idBuf[:]); err != nil {
			return nil, fmt.Errorf("blsbackend: set bls.ID for op %d: %w", opID, err)
		}
		idVec = append(idVec, blsID)
	}

	recovered := bls.Sign{}
	if err := recovered.Recover(sigVec, idVec); err != nil {
		return nil, fmt.Errorf("blsbackend: BLS Recover: %w", err)
	}
	return obft.Signature(recovered.Serialize()), nil
}

// VerifyPartial checks that `partial` is a valid BLS signature on `msg`
// under the public key `pubKeyShare`. Returns false on any deserialization
// or verification failure.
func (s *BLSSigner) VerifyPartial(pubKeyShare []byte, msg []byte, partial obft.Signature) bool {
	if len(pubKeyShare) == 0 || len(msg) == 0 || len(partial) == 0 {
		return false
	}
	pk := bls.PublicKey{}
	if err := pk.Deserialize(pubKeyShare); err != nil {
		return false
	}
	sig := bls.Sign{}
	if err := sig.Deserialize(partial); err != nil {
		return false
	}
	return sig.VerifyByte(&pk, msg)
}

// VerifyAggregate checks that `sig` is a valid full BLS signature on `msg`
// under the cluster's master public key (the validator's pubkey under
// Option A). Returns false on any deserialization or verification failure.
func (s *BLSSigner) VerifyAggregate(clusterPubKey []byte, msg []byte, sig obft.Signature) bool {
	if len(clusterPubKey) == 0 || len(msg) == 0 || len(sig) == 0 {
		return false
	}
	pk := bls.PublicKey{}
	if err := pk.Deserialize(clusterPubKey); err != nil {
		return false
	}
	s2 := bls.Sign{}
	if err := s2.Deserialize(sig); err != nil {
		return false
	}
	return s2.VerifyByte(&pk, msg)
}

// VerifyPartialBatch batch-verifies N (pubKeyShare, msg, sig) tuples in one
// pairing equation via herumi's bls.MultiVerify (random-linear-combination).
// Returns true iff EVERY tuple would individually verify under VerifyPartial.
//
// Each msgs[i] MUST be exactly 32 bytes — herumi concatenates them into one
// buffer of N*32 bytes. OBFT signs over 32-byte signing roots (NR tags and
// the proposer-domain signing root translated by proposerSigner) so this is
// the natural contract.
//
// A false return does NOT identify which tuple failed; callers that need
// per-tuple attribution (Rule-4 evidence at the σ-walk) MUST fall back to a
// per-tuple VerifyPartial loop. Audit finding F4.
//
// Tests that exercise this path MUST skip under `-race` because herumi's
// MultiVerify stores slice pointers in uintptr (eth.go:32-33) which Go's
// checkptr (enabled with -race) flags as unsafe pointer arithmetic. Production
// builds run without checkptr and are unaffected. See multiverify_batch_test.go's
// skipIfRace helper.
func (s *BLSSigner) VerifyPartialBatch(pubKeyShares [][]byte, msgs [][]byte, sigs []obft.Signature) bool {
	n := len(sigs)
	if n == 0 || len(pubKeyShares) != n || len(msgs) != n {
		return false
	}
	concat := make([]byte, 0, n*32)
	sigVec := make([]bls.Sign, n)
	pubVec := make([]bls.PublicKey, n)
	for i := 0; i < n; i++ {
		if len(msgs[i]) != 32 || len(pubKeyShares[i]) == 0 || len(sigs[i]) == 0 {
			return false
		}
		if err := pubVec[i].Deserialize(pubKeyShares[i]); err != nil {
			return false
		}
		if err := sigVec[i].Deserialize(sigs[i]); err != nil {
			return false
		}
		concat = append(concat, msgs[i]...)
	}
	return bls.MultiVerify(sigVec, pubVec, concat)
}
