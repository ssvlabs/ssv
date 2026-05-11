package consensustest

// BLS-realistic byte sizes for the herumi/bls-eth-go-binary library used by
// SSV (BLS12-381 G1 sigs, G2 pubkeys; spec/eth2 conventions). Stub signatures
// in adapters use these sizes so bandwidth measurements in stub mode match
// real-BLS mode — only signing/verification CPU cost differs, not wire size.
//
// Phase 8 (bandwidth instrumentation) will reference these when accounting
// for per-message byte counts; only StubSignatureSize is currently consumed
// (by the OBFT byz patterns that forge σ partial bytes).
const (
	StubSignatureSize = 96 // BLS12-381 G2 signature (compressed)
	StubPublicKeySize = 48 // BLS12-381 G1 public key (compressed)

	// IBE primitive sizes (tlock-style timelock encryption). Production uses
	// drand's age-encrypt-go IBE; the stub matches its envelope.
	StubIBECiphertextOverhead = 48 // IBE element + AEAD overhead per encrypted onion entry
)

// RealisticBlindedBlockBytes is the SSZ-encoded size of a fully-loaded
// Electra blinded beacon block — the actual on-wire payload SSV consensus
// carries during a proposer-duty slot. SSV uses blinded blocks only
// (operator/validator/setup_obft.go / protocol/v2/blockchain/beacon/blind),
// so the consensus value V here represents the SignedBlindedBeaconBlock SSZ
// that flows through Phase-1 bundles (OBFT) and PROPOSE FullData (QBFT).
//
// Measured at 5,152 bytes for a SignedBlindedBeaconBlock containing 8
// aggregate attestations (the post-EIP-7549 Electra cap on Attestations
// per block). Composition:
//   - 96 B BLS signature (signed envelope)
//   - 96 B header (slot + proposer index + parent/state roots)
//   - 396 B body fixed-size baseline (RANDAO + sync aggregate + etc.)
//   - 8 × ~570 B per Electra Attestation (aggregation bitlist + data + sig +
//     committee bits, post-7549 cross-committee aggregation)
//   - ~600 B ExecutionPayloadHeader (transactions/withdrawals replaced by
//     32-byte roots under the MEV-boost blinded convention; no execution
//     payload bytes on-wire)
//
// Empty / minimal block lands at ~1.15 KB. Production blocks typically sit
// at 1–5 KB depending on attestation aggregation; we model the upper end
// (8-att fully-loaded) so bandwidth measurements aren't biased low.
//
// Note: a placeholder ~15-byte value (the simulator's prior canonValues /
// startValue) dramatically understates OBFT's per-commit V replication
// across σ-state layers — see protocol/v2/consensustest/obft/sizes.go
// commitSize for where V bytes get fanned out per layer.
const RealisticBlindedBlockBytes = 5152

// MakeRealisticBlindedBlockValue returns a deterministic byte slice sized
// to RealisticBlindedBlockBytes. The leading byte is set to `seed` so callers
// can produce *distinct* candidate V's per layer/round, matching production
// where each leader picks an independent block from their relay/beacon. The
// remaining bytes are zero — content doesn't affect protocol behavior
// (BLS verify treats V as opaque bytes), only size and distinctness do.
//
// Returns a fresh slice each call; callers may mutate without affecting
// other holders.
func MakeRealisticBlindedBlockValue(seed byte) []byte {
	v := make([]byte, RealisticBlindedBlockBytes)
	v[0] = seed
	return v
}
