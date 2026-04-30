package tbft

// ThresholdIBE is the encryption primitive TBFT relies on: identity-based
// encryption (IBE) where decrypting a ciphertext bound to tag T requires
// the BLS signature on T under the cluster's threshold key.
//
// Under the chosen Option-A design (reuse the validator's existing threshold
// BLS key as the IBE trust anchor), the "decryption key" for tag T is just
// the full BLS signature on T — produced by aggregating ≥ 2f+1 partial
// signatures from the cluster's operators. Producing a partial signature
// and aggregating partials are NOT IBE-specific operations; they live in
// the Signer interface (see signer.go).
//
// Implementations:
//   - StubIBE (this package, ibe_stub.go) — placeholder for protocol-level
//     tests that don't exercise real cryptography.
//   - tlock-backed implementation (added later, integrating drand/tlock).
type ThresholdIBE interface {
	// Encrypt produces a ciphertext over `plaintext` bound to `tag`.
	// `clusterPubKey` is the validator's BLS pubkey (the IBE trust anchor).
	// To decrypt, one must present a BLS signature on `tag` produced under
	// the corresponding cluster threshold key.
	Encrypt(clusterPubKey []byte, tag []byte, plaintext []byte) (ciphertext []byte, err error)

	// Decrypt opens `ciphertext` using `key` (an aggregated full BLS sig
	// on the tag the ciphertext was bound to). Returns the plaintext.
	Decrypt(ciphertext []byte, key []byte) (plaintext []byte, err error)
}
