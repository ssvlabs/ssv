package obft

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// ThresholdIBE is the encryption primitive OBFT relies on: identity-based
// encryption (IBE) where decrypting a ciphertext bound to tag T requires
// the BLS signature on T under the cluster's threshold key.
//
// Per spec §Cryptographic primitive, the production primitive is threshold
// IBE / signature-based witness encryption — the same family drand/tlock
// uses. The cluster's IBE keypair is distinct from its V-keypair (different
// cryptographic backend so the IBE primitive can use its expected DST), but
// shares the same threshold qEnc = qV = 2f+1.
//
// OBFT wraps σ partials at layer k > 0 in k levels of IBE encryption (chain
// depth grows with the layer index — per spec §Phase 2, L_0 is plaintext,
// L_1 has 1 level, ..., L_{K-1} has K-1 levels at the deepest):
//
//	layer k:  E_{nr_tag_0}( ... E_{nr_tag_{k-1}}( σ_i^V(V_{L_k}) ) ... )
//
// The ThresholdIBE interface is for a single layer of IBE; chained encryption
// is implemented in the obft package as a per-layer-k wrapper around k calls
// (see chainEncryptForLayer / chainDecryptForLayer in instance.go).
//
// Implementations:
//   - StubIBE (this file) — placeholder for protocol-level tests.
//   - tlock-backed implementation in obft/blsbackend (post-migration).
type ThresholdIBE interface {
	// Encrypt produces a ciphertext over `plaintext` bound to `tag`.
	// `clusterPubKey` is the cluster's IBE pubkey (or, under Option A, the
	// validator's BLS pubkey via the DST trick). To decrypt, one must
	// present a BLS signature on `tag` produced under the corresponding
	// cluster threshold key.
	Encrypt(clusterPubKey []byte, tag []byte, plaintext []byte) (ciphertext []byte, err error)

	// Decrypt opens `ciphertext` using `key` (an aggregated full BLS sig
	// on the tag the ciphertext was bound to). Returns the plaintext.
	Decrypt(ciphertext []byte, key []byte) (plaintext []byte, err error)
}

// StubIBE is a placeholder ThresholdIBE for protocol-level tests. It does
// NOT provide cryptographic security; it exists to let the protocol logic
// (onion construction, decryption walk, layer aggregation) be tested
// without a real IBE library wired in.
//
// The stub assumes the "decryption key" presented to Decrypt is whatever
// StubSigner.AggregatePartials produces for the matching tag.
//
// Stub format:
//
//	Encrypt -> []byte{0x03} || tag-msg-id(8) || sha256(tag||plaintext)(32) || plaintext
//	Decrypt -> verifies key is StubSigner aggregate (0x11 || msg-id || quorum)
//	           where msg-id matches the embedded tag-msg-id; returns plaintext.
type StubIBE struct {
	// Quorum is the threshold (2f+1) for any tag. Decrypt rejects keys
	// whose embedded quorum size doesn't match this value.
	Quorum int
}

// NewStubIBE creates a stub IBE configured for a cluster with the given
// quorum threshold.
func NewStubIBE(quorum int) *StubIBE {
	return &StubIBE{Quorum: quorum}
}

const stubVersionCipher = 0x03

// Encrypt returns []byte{0x03} || tag-msg-id(8) || sha256(tag||plaintext)(32) || plaintext.
// clusterPubKey is unused in the stub.
func (s *StubIBE) Encrypt(_ []byte, tag []byte, plaintext []byte) ([]byte, error) {
	if len(tag) == 0 {
		return nil, errors.New("obft stub ibe: empty tag")
	}
	tagID := msgHash(tag)
	mac := sha256.Sum256(append(append([]byte{}, tag...), plaintext...))

	out := make([]byte, 0, 1+8+32+len(plaintext))
	out = append(out, stubVersionCipher)
	out = append(out, tagID[:]...)
	out = append(out, mac[:]...)
	out = append(out, plaintext...)
	return out, nil
}

// Decrypt verifies the key (a StubSigner aggregate) corresponds to the tag
// embedded in the ciphertext, then returns the plaintext.
//
// `key` must be a StubSigner aggregate-sig in the format
// 0x11 || msg-id(8) || quorum(4) — produced by StubSigner.AggregatePartials
// over partial signatures of the IBE tag.
func (s *StubIBE) Decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	if len(ciphertext) < 1+8+32 || ciphertext[0] != stubVersionCipher {
		return nil, errors.New("obft stub ibe: malformed ciphertext")
	}
	cTagID := ciphertext[1:9]
	plaintextStart := 1 + 8 + 32
	if plaintextStart > len(ciphertext) {
		return nil, errors.New("obft stub ibe: ciphertext truncated")
	}

	if len(key) != 1+8+4 || key[0] != stubVersionAggregateSig {
		return nil, errors.New("obft stub ibe: malformed key (expected StubSigner aggregate)")
	}
	kTagID := key[1:9]
	if !bytes.Equal(cTagID, kTagID) {
		return nil, errors.New("obft stub ibe: tag mismatch (key signs different tag than ciphertext is bound to)")
	}
	gotQ := binary.BigEndian.Uint32(key[9:13])
	if int(gotQ) != s.Quorum {
		return nil, fmt.Errorf("obft stub ibe: key quorum %d != configured quorum %d", gotQ, s.Quorum)
	}

	return ciphertext[plaintextStart:], nil
}
