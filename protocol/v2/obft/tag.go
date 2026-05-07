package obft

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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
// partial signatures (NR partials in KindCommit) when they are NR-committed at
// layer `layer`.
//
// `layer` is in [0, K-2]; there is no NR tag for L_{K-1} since no layer
// follows it.
func NoQuorumTag(clusterID [32]byte, height Height, layer int) []byte {
	// Caller MUST pass a non-negative in-range layer; out-of-range values
	// would either wrap silently (negative → very large uint32) or break
	// per-layer tag uniqueness. Real OBFT configs use K ≤ 32 (MaxLayers in
	// the wire package); panic on misuse rather than silently corrupt the
	// tag. Defense-in-depth: validators / decoders also bound layer to
	// MaxLayers before this function is reached.
	if layer < 0 || layer > 1<<31-1 {
		panic(fmt.Sprintf("obft: NoQuorumTag layer %d out of range", layer))
	}
	h := sha256.New()
	h.Write(domainNoQuorum)
	h.Write(clusterID[:])

	var heightBytes [8]byte
	binary.BigEndian.PutUint64(heightBytes[:], uint64(height))
	h.Write(heightBytes[:])

	var layerBytes [4]byte
	binary.BigEndian.PutUint32(layerBytes[:], uint32(layer)) //nolint:gosec // bounds-checked above
	h.Write(layerBytes[:])

	return h.Sum(nil)
}
