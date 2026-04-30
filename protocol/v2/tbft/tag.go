package tbft

import (
	"crypto/sha256"
	"encoding/binary"
)

// Tag construction.
//
// IBE tags must uniquely bind every relevant context bit so that ciphertexts
// from one instance cannot be replayed in another, and so that a partial
// signature produced for one purpose cannot serve as a decryption witness
// for another. We use SHA-256 over a structured encoding that includes:
//
//   - a domain-separation prefix per tag kind
//   - the cluster identifier
//   - the consensus-instance height (the slot)
//   - the layer index (where applicable)
//
// Hashed output is used as the IBE label. 32 bytes is comfortable for
// BLS12-381 hash-to-curve.

// Domain-separation prefixes. Each tag kind gets a distinct prefix so the
// tag space cannot collide across kinds. ASCII strings used for human
// readability when debugging.
var (
	domainNoQuorum = []byte("TBFT/no-quorum/v1")
	// domainPrimaryLeader and domainBackupLeader exist for the TBFT2-style
	// configuration where layer 0 is "primary" and layer 1 is "backup".
	// The protocol code uses NoQuorumTag uniformly; these are reserved
	// for documentation / future variants where the per-layer semantics
	// might differ from "no-quorum-at-layer-(k-1)".
)

// NoQuorumTag returns the IBE label for "no positive quorum was reached at
// layer `layer` of the instance identified by (clusterID, height)".
//
// Decryption of layer `layer + 1` of any operator's onion requires
// aggregating 2f+1 partial signatures on this tag. Operators broadcast
// such partial signatures (NonReceiptAttestation) when they did not
// receive a valid candidate at layer `layer`.
//
// Note that layer 0's onion is plaintext (no tag, always openable) — there
// is no NoQuorumTag(... , -1). Tags exist for layer indices 0..K-2: a tag
// for layer k unlocks layer k+1.
func NoQuorumTag(clusterID [32]byte, height Height, layer int) []byte {
	h := sha256.New()
	h.Write(domainNoQuorum)
	h.Write(clusterID[:])

	var heightBytes [8]byte
	binary.BigEndian.PutUint64(heightBytes[:], uint64(height))
	h.Write(heightBytes[:])

	var layerBytes [4]byte
	binary.BigEndian.PutUint32(layerBytes[:], uint32(layer))
	h.Write(layerBytes[:])

	return h.Sum(nil)
}

// LayerTag returns the IBE tag the ciphertext at layer `layer` is encrypted
// under. By construction this is NoQuorumTag(layer-1) — i.e. layer k's
// ciphertext decrypts when "no quorum at layer k-1" is proven.
//
// Layer 0 returns nil (plaintext, no tag).
func (c *Config) LayerTag(layer int) []byte {
	if layer == 0 {
		return nil
	}
	return NoQuorumTag(c.ClusterID, c.Height, layer-1)
}
