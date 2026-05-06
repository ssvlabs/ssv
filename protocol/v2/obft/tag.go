package obft

import (
	"crypto/sha256"
	"encoding/binary"
)

// Tag construction.
//
// Each NR tag must uniquely bind (slot, cluster, layer) so that ciphertexts
// from one instance cannot be replayed in another and a partial signature
// produced for one purpose cannot serve as a decryption witness for another.
// We use SHA-256 over a structured encoding that includes a domain-separation
// prefix per tag kind.
//
// Per spec §Setting: nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")
// for k ∈ {0, ..., K-2}. The deepest layer (L_{K-1}) has no NR tag — there
// is no further layer to advance to.

var domainNoQuorum = []byte("OBFT/no-quorum/v1")

// NoQuorumTag returns the IBE label for "no positive quorum was reached at
// layer `layer` of the instance identified by (clusterID, height)".
//
// Decryption of layer `layer + 1`'s outermost-wrap chained ciphertext requires
// aggregating qEnc partial signatures on this tag. Operators broadcast such
// partial signatures (NR partials in KindNR) when they are NR-committed at
// layer `layer`.
//
// `layer` is in [0, K-2]; there is no NR tag for L_{K-1} since no layer
// follows it.
func NoQuorumTag(clusterID [32]byte, height Height, layer int) []byte {
	h := sha256.New()
	h.Write(domainNoQuorum)
	h.Write(clusterID[:])

	var heightBytes [8]byte
	binary.BigEndian.PutUint64(heightBytes[:], uint64(height))
	h.Write(heightBytes[:])

	var layerBytes [4]byte
	// Layer is a small non-negative onion-layer index (≤ K ≤ ~13 for SSV);
	// well within uint32 range.
	binary.BigEndian.PutUint32(layerBytes[:], uint32(layer)) //nolint:gosec // small non-negative
	h.Write(layerBytes[:])

	return h.Sum(nil)
}
