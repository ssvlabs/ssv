// Package obft holds the super-generic cryptography primitives and bare
// type aliases shared between the bare OBFT and 2abOBFT protocol
// implementations. Protocol-specific state machines, wire formats, data
// structures, validation, evidence types, and EKM coordinators live in
// the sub-packages:
//
//   - protocol/v2/obft/base/  — bare OBFT (single-round, per docs/OBFT.md)
//   - protocol/v2/obft/twoab/ — 2abOBFT (Phase 2a/2b split, per docs/2abOBFT.md)
//
// The shared surface here is intentionally minimal: type aliases for
// participant / instance / payload identifiers, the BLS Signer interface,
// the ThresholdIBE interface, and the deterministic NR-tag derivation
// function. The blsbackend/ sub-package provides production implementations
// of Signer and ThresholdIBE.
//
// This package is intentionally independent of github.com/ssvlabs/ssv-spec.
// SSV-specific integration lives in protocol/v2/ssv/runner/obft.
package obft

// OperatorID identifies a participant in the cluster. Matches the uint64 ID
// space used elsewhere in SSV but is not coupled to spec types.
type OperatorID uint64

// Height is a generic instance identifier — typically a slot number when used
// in SSV but otherwise opaque. The protocol does not interpret it; tag
// construction binds it into the IBE labels to prevent cross-instance replay.
type Height uint64

// Value is the candidate bytes being agreed upon (e.g. a serialized blinded
// block in the SSV proposer-duty case). The protocol treats it as opaque.
type Value []byte

// Signature is a BLS signature, either a partial (operator share) or a
// reconstructed full signature.
type Signature []byte
