package blsbackend

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// SignerGatedIBE is a NON-CRYPTOGRAPHIC ThresholdIBE that enforces the
// protocol access gate using a Signer for signature verification. Its
// purpose is to enable end-to-end protocol tests with real BLS keys
// before drand/tlock integration is wired up. It does NOT provide
// cryptographic confidentiality — the plaintext is embedded directly
// in the ciphertext.
//
// Behavior contract (matches the abstract IBE behavior the protocol
// depends on):
//
//   - Encrypt(pubkey, tag, plaintext) → ciphertext binding (tag, plaintext).
//   - Decrypt(ciphertext, key) → plaintext IFF `key` is a valid BLS
//     aggregate signature on the tag the ciphertext is bound to,
//     verifiable under ClusterPubKey by the embedded Verifier.
//   - Wrong key (different tag, wrong cluster, malformed) → error.
//
// What's stubbed:
//
//   - Confidentiality. Anyone with the ciphertext can read the plaintext
//     bytes directly; SignerGatedIBE only enforces the access gate, not
//     the confidentiality property a true IBE provides.
//
// Production deployments must replace this with a true cryptographic
// IBE (drand/tlock or equivalent). The protocol code is written against
// the ThresholdIBE interface so swapping is a one-line change at the
// adapter boundary.
//
// Wire format:
//
//	0x04 || tag-len(uint16) || tag || plaintext-len(uint32) || plaintext
type SignerGatedIBE struct {
	// Verifier is used to check that decryption keys are valid BLS sigs
	// over the tag the ciphertext is bound to.
	Verifier obft.Signer

	// ClusterPubKey is the master pubkey under which decryption keys
	// (BLS aggregate sigs) must verify. For Option A this is the
	// validator's BLS pubkey.
	ClusterPubKey []byte
}

// NewSignerGatedIBE constructs a SignerGatedIBE configured with the given
// Signer for key verification.
func NewSignerGatedIBE(verifier obft.Signer, clusterPubKey []byte) *SignerGatedIBE {
	return &SignerGatedIBE{
		Verifier:      verifier,
		ClusterPubKey: clusterPubKey,
	}
}

const sigGatedIBEVersion byte = 0x04

// Encrypt embeds (tag, plaintext) into a ciphertext format that Decrypt
// can parse. The `pubkey` argument is unused (the trust anchor is
// configured at construction via ClusterPubKey).
func (s *SignerGatedIBE) Encrypt(_ []byte, tag []byte, plaintext []byte) ([]byte, error) {
	if len(tag) == 0 {
		return nil, errors.New("blsbackend: SignerGatedIBE.Encrypt: empty tag")
	}
	if len(tag) > 0xFFFF {
		return nil, fmt.Errorf("blsbackend: SignerGatedIBE.Encrypt: tag too long (%d > 65535)", len(tag))
	}
	if len(plaintext) > 0xFFFFFFFF {
		return nil, fmt.Errorf("blsbackend: SignerGatedIBE.Encrypt: plaintext too long")
	}

	out := make([]byte, 0, 1+2+len(tag)+4+len(plaintext))
	out = append(out, sigGatedIBEVersion)

	var tagLen [2]byte
	// Bounds-checked above (len(tag) ≤ 0xFFFF / len(plaintext) ≤ 0xFFFFFFFF).
	binary.BigEndian.PutUint16(tagLen[:], uint16(len(tag))) //nolint:gosec // bounds-checked
	out = append(out, tagLen[:]...)
	out = append(out, tag...)

	var plLen [4]byte
	binary.BigEndian.PutUint32(plLen[:], uint32(len(plaintext))) //nolint:gosec // bounds-checked
	out = append(out, plLen[:]...)
	out = append(out, plaintext...)
	return out, nil
}

// Decrypt verifies `key` is a valid signature on the ciphertext's tag and,
// if so, returns the embedded plaintext. Returns an error if the key is
// not a valid sig on the bound tag or if the ciphertext is malformed.
func (s *SignerGatedIBE) Decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	tag, plaintext, err := parseSignerGatedIBE(ciphertext)
	if err != nil {
		return nil, err
	}

	if s.Verifier == nil {
		return nil, errors.New("blsbackend: SignerGatedIBE.Decrypt: no Verifier configured")
	}
	if !s.Verifier.VerifyAggregate(s.ClusterPubKey, tag, key) {
		return nil, errors.New("blsbackend: SignerGatedIBE.Decrypt: key is not a valid signature on the ciphertext's tag")
	}

	// Return a defensive copy so callers can't mutate the original ciphertext bytes.
	out := make([]byte, len(plaintext))
	copy(out, plaintext)
	return out, nil
}

func parseSignerGatedIBE(ciphertext []byte) (tag []byte, plaintext []byte, err error) {
	if len(ciphertext) < 1+2 {
		return nil, nil, errors.New("blsbackend: SignerGatedIBE: ciphertext truncated (header)")
	}
	if ciphertext[0] != sigGatedIBEVersion {
		return nil, nil, fmt.Errorf("blsbackend: SignerGatedIBE: bad version byte 0x%02x", ciphertext[0])
	}
	tagLen := int(binary.BigEndian.Uint16(ciphertext[1:3]))
	tagStart := 3
	tagEnd := tagStart + tagLen
	if tagEnd+4 > len(ciphertext) {
		return nil, nil, errors.New("blsbackend: SignerGatedIBE: ciphertext truncated (tag)")
	}
	tag = ciphertext[tagStart:tagEnd]

	plLenBytes := ciphertext[tagEnd : tagEnd+4]
	plLen := int(binary.BigEndian.Uint32(plLenBytes))
	plStart := tagEnd + 4
	plEnd := plStart + plLen
	if plEnd > len(ciphertext) {
		return nil, nil, errors.New("blsbackend: SignerGatedIBE: ciphertext truncated (plaintext)")
	}
	plaintext = ciphertext[plStart:plEnd]
	return tag, plaintext, nil
}
