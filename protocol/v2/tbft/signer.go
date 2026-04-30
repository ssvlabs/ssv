package tbft

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// Signer is the BLS-style threshold signing primitive TBFT uses to:
//
//   - Produce per-operator partial signatures on candidate values
//     (these go inside an Onion, encrypted under per-layer tags).
//   - Produce per-operator partial signatures on no-quorum tags
//     (these are NonReceiptAttestations, and aggregating 2f+1 of them
//     yields the IBE decryption key for the next layer).
//   - Aggregate 2f+1 partials into a full signature, both for the
//     reconstructed validator signature on the decided value and for
//     the IBE decryption key derivation.
//
// For Option A (reuse the validator's existing threshold BLS key as the
// IBE trust anchor), this primitive is just the standard BLS threshold
// signing operation, identical for tag-signing and value-signing — the
// underlying message is the only difference.
type Signer interface {
	// SignPartial signs `msg` using `share` (operator's secret key share)
	// and returns the partial signature. For Option A this is exactly a
	// herumi/bls partial signature.
	SignPartial(share []byte, msg []byte) (Signature, error)

	// AggregatePartials reconstructs a full signature from a set of
	// partial signatures, keyed by operator ID for Lagrange interpolation.
	// At least q (= 2f+1) distinct operator partials are required.
	AggregatePartials(partials map[OperatorID]Signature) (Signature, error)

	// VerifyPartial checks that `partial` is a valid partial signature on
	// `msg` produced with the operator's share corresponding to `pubKeyShare`.
	// (This is optional; the protocol can run without per-partial
	// verification if the trust model is purely byzantine-aware aggregate
	// verification at the end.)
	VerifyPartial(pubKeyShare []byte, msg []byte, partial Signature) bool

	// VerifyAggregate checks that `sig` is a valid full signature on `msg`
	// under `clusterPubKey` (the aggregate/master public key).
	VerifyAggregate(clusterPubKey []byte, msg []byte, sig Signature) bool
}

// StubSigner is a non-cryptographic Signer for protocol-level testing,
// mirroring the StubIBE approach. Properties:
//
//   - Partials are H(operatorShare || msg), so distinct shares produce
//     distinct partials, and the same share + msg always produces the
//     same partial (deterministic).
//   - Aggregates are H("agg" || msg || quorum), so any 2f+1 subset of
//     valid partials on the same message yields the SAME aggregate —
//     this is the crucial property real BLS threshold reconstruction has
//     and which the protocol depends on (different quorum subsets must
//     yield identical "decided signatures" for cluster-wide consistency).
//   - VerifyPartial just reproduces the partial and compares.
//   - VerifyAggregate reproduces the expected aggregate and compares.
//
// Format:
//
//	partial    = []byte{0x10} || H(share)[:8] || sha256(msg)[:8] || sha256(share || msg)
//	aggregate  = []byte{0x11} || sha256(msg)[:8] || quorum(big-endian 4 bytes)
type StubSigner struct {
	Quorum int
}

func NewStubSigner(quorum int) *StubSigner {
	return &StubSigner{Quorum: quorum}
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

// SignPartial produces []byte{0x10} || share-id-tag || msg-tag || H(share || msg).
func (s *StubSigner) SignPartial(share []byte, msg []byte) (Signature, error) {
	if len(share) == 0 {
		return nil, errors.New("stub signer: empty share")
	}
	if len(msg) == 0 {
		return nil, errors.New("stub signer: empty message")
	}
	shareID := shareHash(share)
	msgID := msgHash(msg)
	full := sha256.Sum256(append(append([]byte{}, share...), msg...))

	out := make([]byte, 0, 1+8+8+32)
	out = append(out, stubVersionPartialSig)
	out = append(out, shareID[:]...)
	out = append(out, msgID[:]...)
	out = append(out, full[:]...)
	return out, nil
}

// AggregatePartials verifies all partials are well-formed, share the same
// msg-tag, and have ≥ Quorum distinct share-ids. Returns the deterministic
// aggregate ([]byte{0x11} || msg-tag || quorum).
func (s *StubSigner) AggregatePartials(partials map[OperatorID]Signature) (Signature, error) {
	if len(partials) < s.Quorum {
		return nil, fmt.Errorf("stub signer: need %d partials, got %d", s.Quorum, len(partials))
	}

	var canonicalMsgID []byte
	seenShareIDs := make(map[string]bool)
	for opID, p := range partials {
		if len(p) != 1+8+8+32 || p[0] != stubVersionPartialSig {
			return nil, fmt.Errorf("stub signer: malformed partial for op %d", opID)
		}
		shareID := p[1:9]
		msgID := p[9:17]
		if canonicalMsgID == nil {
			canonicalMsgID = append([]byte{}, msgID...)
		} else if !bytes.Equal(msgID, canonicalMsgID) {
			return nil, errors.New("stub signer: partials sign different messages")
		}
		if seenShareIDs[string(shareID)] {
			return nil, fmt.Errorf("stub signer: duplicate share-id (op %d)", opID)
		}
		seenShareIDs[string(shareID)] = true
	}

	out := make([]byte, 0, 1+8+4)
	out = append(out, stubVersionAggregateSig)
	out = append(out, canonicalMsgID...)
	var qBytes [4]byte
	binary.BigEndian.PutUint32(qBytes[:], uint32(s.Quorum))
	out = append(out, qBytes[:]...)
	return out, nil
}

// VerifyPartial recomputes the expected partial and compares.
//
// In the stub, "pubKeyShare" must be the same bytes used as the share in
// SignPartial (since the stub doesn't have a real keypair concept).
func (s *StubSigner) VerifyPartial(pubKeyShare []byte, msg []byte, partial Signature) bool {
	expected, err := s.SignPartial(pubKeyShare, msg)
	if err != nil {
		return false
	}
	return bytes.Equal(expected, partial)
}

// VerifyAggregate checks that `sig` matches what AggregatePartials would
// produce for `msg` (clusterPubKey is unused in the stub).
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
