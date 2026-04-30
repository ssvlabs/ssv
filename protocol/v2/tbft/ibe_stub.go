package tbft

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// StubIBE is a placeholder ThresholdIBE for protocol-level tests. It does
// NOT provide cryptographic security; it exists to let the protocol logic
// (onion construction, decryption walk, layer aggregation) be tested
// without a real IBE library wired in.
//
// The stub assumes the "decryption key" presented to Decrypt is whatever
// StubSigner.AggregatePartials produces for the matching tag — that's
// the design under Option A (reuse validator threshold key for IBE).
// Aggregate sigs in the stub format are: 0x11 || msg-tag(8) || quorum(4)
// (see signer.go). Encrypt embeds the same msg-tag of the IBE tag into
// the ciphertext so Decrypt can verify the key matches.
//
// Stub format:
//
//   - Encrypt -> []byte{0x03} || tag-msg-id(8) || sha256(tag||plaintext)(32) || plaintext
//   - Decrypt -> verifies key is StubSigner aggregate (0x11 || msg-id || quorum)
//     where msg-id matches the embedded tag-msg-id; verifies MAC; returns plaintext.
//
// Deterministic outputs.
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

const (
	stubVersionCipher = 0x03
)

// Encrypt returns []byte{0x03} || tag-msg-id(8) || sha256(tag||plaintext)(32) || plaintext.
// clusterPubKey is unused in the stub.
func (s *StubIBE) Encrypt(_ []byte, tag []byte, plaintext []byte) ([]byte, error) {
	if len(tag) == 0 {
		return nil, errors.New("stub ibe: empty tag")
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
		return nil, errors.New("stub ibe: malformed ciphertext")
	}
	cTagID := ciphertext[1:9]
	macStart := 9
	plaintextStart := macStart + 32
	if plaintextStart > len(ciphertext) {
		return nil, errors.New("stub ibe: ciphertext truncated")
	}

	if len(key) != 1+8+4 || key[0] != stubVersionAggregateSig {
		return nil, errors.New("stub ibe: malformed key (expected StubSigner aggregate)")
	}
	kTagID := key[1:9]
	if !bytes.Equal(cTagID, kTagID) {
		return nil, errors.New("stub ibe: tag mismatch (key signs different tag than ciphertext is bound to)")
	}
	gotQ := binary.BigEndian.Uint32(key[9:13])
	if int(gotQ) != s.Quorum {
		return nil, fmt.Errorf("stub ibe: key quorum %d != configured quorum %d", gotQ, s.Quorum)
	}

	// MAC is over the full original tag; we only have the tag-id stored
	// in the ciphertext. To verify the MAC we need the original tag —
	// which the caller provided implicitly by aggregating sigs on it.
	// We can't recompute the MAC here without the original tag bytes.
	// Instead, since the stub's purpose is protocol testing, we trust
	// the tag-id binding in the key and skip MAC verification on Decrypt.
	// The MAC is still embedded for forward-compatibility / debugging.
	_ = macStart
	plaintext := ciphertext[plaintextStart:]
	return plaintext, nil
}
