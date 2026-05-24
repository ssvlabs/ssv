package obft

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"sync"
)

// Signer is the BLS-style threshold signing primitive OBFT uses to:
//
//   - Produce per-operator partial signatures on candidate values
//     (these go inside the per-layer onion entries of a KindCommit message;
//     for L_0 plaintext, for k > 0 wrapped in chained IBE encryption).
//   - Produce the layer leader's partial signature on V at Phase 1
//     (head-start contribution to the σ-pool — the `σ_L^V` in spec terms).
//   - Produce per-operator partial signatures on NR tags (these are the
//     NR partials carried alongside σ partials in KindCommit; aggregating
//     to qEnc unlocks layer-k+1 chained decryption).
//   - Aggregate qV / qEnc partials into a full signature, both for the
//     reconstructed V signature and for IBE decryption-key derivation.
//
// A Signer is bound to a single operator's secret share at construction
// time — the share never travels through the call stack as a parameter.
// This matches SSV's existing `ekm.BeaconSigner` shape.
//
// AggregatePartials / VerifyPartial / VerifyAggregate operations don't need
// the bound share — they're stateless wrt operator identity and the same
// Signer instance can serve all three regardless of which share it was
// constructed for.
type Signer interface {
	// SignPartial signs `msg` with the operator's secret share that this
	// Signer was constructed against, and returns the partial signature.
	SignPartial(msg []byte) (Signature, error)

	// AggregatePartials reconstructs a full signature from a set of
	// partial signatures, keyed by operator ID for Lagrange interpolation.
	// At least q (= 2f+1) distinct operator partials are required.
	AggregatePartials(partials map[OperatorID]Signature) (Signature, error)

	// VerifyPartial checks that `partial` is a valid partial signature on
	// `msg` produced with the operator's share corresponding to `pubKeyShare`.
	VerifyPartial(pubKeyShare []byte, msg []byte, partial Signature) bool

	// VerifyAggregate checks that `sig` is a valid full signature on `msg`
	// under `clusterPubKey` (the aggregate / master public key).
	VerifyAggregate(clusterPubKey []byte, msg []byte, sig Signature) bool
}

// StubSigner is a non-cryptographic Signer for protocol-level testing,
// mirroring the StubIBE approach. Properties:
//
//   - Partials are deterministic (sha256(share || sha256(msg))), so distinct
//     shares produce distinct partials.
//   - Aggregates are deterministic and depend only on (msg, quorum) — any
//     2f+1 subset of valid partials on the same message yields the SAME
//     aggregate. This is the property real BLS threshold reconstruction has
//     and which the protocol depends on.
//   - VerifyPartial and VerifyAggregate just reproduce and compare.
//
// Format:
//
//	partial    = []byte{0x10} || H(share)[:8] || sha256(msg)[:8] || sha256(share || sha256(msg))
//	aggregate  = []byte{0x11} || sha256(msg)[:8] || quorum(big-endian 4 bytes)
//
// "Share" is just an opaque identity tag in the stub — typically the
// operator's ID byte. Each operator constructs their own StubSigner with
// their own share bytes.
type StubSigner struct {
	Quorum int
	share  []byte
}

// NewStubSigner constructs a StubSigner bound to `share`. `share` may be
// empty for verify-only / aggregate-only use cases; SignPartial then
// returns an error.
func NewStubSigner(quorum int, share []byte) *StubSigner {
	return &StubSigner{Quorum: quorum, share: share}
}

const (
	stubVersionPartialSig   = 0x10
	stubVersionAggregateSig = 0x11
)

func msgHash(msg []byte) [8]byte {
	h := sha256.Sum256(msg)
	var out [8]byte
	copy(out[:], h[:8])
	return out
}

func shareHash(share []byte) [8]byte {
	h := sha256.Sum256(share)
	var out [8]byte
	copy(out[:], h[:8])
	return out
}

func (s *StubSigner) SignPartial(msg []byte) (Signature, error) {
	if len(s.share) == 0 {
		return nil, errors.New("obft stub signer: signer has no share bound (verify-only)")
	}
	if len(msg) == 0 {
		return nil, errors.New("obft stub signer: empty message")
	}
	return stubPartialFor(s.share, msg), nil
}

// sha256Pool reuses streaming hashers for the share-bound tag below so each
// partial avoids allocating a hasher (and any input concatenation) on the hot
// path. The (potentially ≈5 KB) value is hashed only once — for msgID — and
// only its 32-byte digest is fed here, so the tag hash itself is tiny.
var sha256Pool = sync.Pool{New: func() any { return sha256.New() }}

func stubPartialFor(share, msg []byte) Signature {
	shareID := shareHash(share)
	// Hash the (potentially large) value exactly once. The digest serves as
	// both msgID and the input to the share-bound tag: hashing the value a
	// second time for the tag would be pure waste, since the stub only needs a
	// value that is deterministic and distinct per (share, msg), and
	// sha256(share || sha256(msg)) has that property. (Real-BLS CPU realism is
	// behind the real_bls build tag, not the stub.)
	msgDigest := sha256.Sum256(msg)
	msgID := msgDigest[:8]

	h := sha256Pool.Get().(hash.Hash)
	h.Reset()
	h.Write(share)
	h.Write(msgDigest[:])
	var full [32]byte
	h.Sum(full[:0])
	sha256Pool.Put(h)

	out := make([]byte, 0, 1+8+8+32)
	out = append(out, stubVersionPartialSig)
	out = append(out, shareID[:]...)
	out = append(out, msgID...)
	out = append(out, full[:]...)
	return out
}

func (s *StubSigner) AggregatePartials(partials map[OperatorID]Signature) (Signature, error) {
	if len(partials) < s.Quorum {
		return nil, fmt.Errorf("obft stub signer: need %d partials, got %d", s.Quorum, len(partials))
	}

	var canonicalMsgID []byte
	seenShareIDs := make(map[string]bool)
	for opID, p := range partials {
		if len(p) != 1+8+8+32 || p[0] != stubVersionPartialSig {
			return nil, fmt.Errorf("obft stub signer: malformed partial for op %d", opID)
		}
		shareID := p[1:9]
		msgID := p[9:17]
		if canonicalMsgID == nil {
			canonicalMsgID = append([]byte{}, msgID...)
		} else if !bytes.Equal(msgID, canonicalMsgID) {
			return nil, errors.New("obft stub signer: partials sign different messages")
		}
		if seenShareIDs[string(shareID)] {
			return nil, fmt.Errorf("obft stub signer: duplicate share-id (op %d)", opID)
		}
		seenShareIDs[string(shareID)] = true
	}

	out := make([]byte, 0, 1+8+4)
	out = append(out, stubVersionAggregateSig)
	out = append(out, canonicalMsgID...)
	var qBytes [4]byte
	// Quorum is a small non-negative cluster-size-derived int (≤ 2f+1 ≤ ~10
	// in realistic clusters), well within uint32 range.
	binary.BigEndian.PutUint32(qBytes[:], uint32(s.Quorum)) //nolint:gosec // small non-negative
	out = append(out, qBytes[:]...)
	return out, nil
}

func (s *StubSigner) VerifyPartial(pubKeyShare []byte, msg []byte, partial Signature) bool {
	if len(pubKeyShare) == 0 || len(msg) == 0 {
		return false
	}
	return bytes.Equal(stubPartialFor(pubKeyShare, msg), partial)
}

func (s *StubSigner) VerifyAggregate(_ []byte, msg []byte, sig Signature) bool {
	if len(sig) != 1+8+4 || sig[0] != stubVersionAggregateSig {
		return false
	}
	expectedMsgID := msgHash(msg)
	if !bytes.Equal(sig[1:9], expectedMsgID[:]) {
		return false
	}
	gotQ := binary.BigEndian.Uint32(sig[9:13])
	return int(gotQ) == s.Quorum
}
