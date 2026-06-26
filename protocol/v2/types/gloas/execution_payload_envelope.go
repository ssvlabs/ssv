package gloas

import (
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Regenerate with `go generate ./...`. -path is the package dir (not just this file) so sszgen resolves
// the sibling gloas BuilderIndex the envelope references; --objs limits output to the blinded envelope,
// collected into its own _encoding.go. Includes track go-eth2-client via `go list -m`.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path . --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/electra,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/bellatrix --objs BlindedExecutionPayloadEnvelope --output ./execution_payload_envelope_encoding.go"

// BlindedExecutionPayloadEnvelope is the blinded form of the Gloas ExecutionPayloadEnvelope that the §6
// envelope-signing duty signs (SIP #94 §6): the full `payload` is replaced by
// PayloadRoot = hash_tree_root(payload). By SSZ Container positional merkleization its hash-tree root
// equals the full envelope's, so a BLS signature over the blinded signing root is valid for the full
// SignedExecutionPayloadEnvelope. It rides in EnvelopeConsensusData.DataSSZ; blinding keeps that QBFT
// value bounded — a few hundred bytes rather than the full payload's hundreds of KB to ~MB.
type BlindedExecutionPayloadEnvelope struct {
	PayloadRoot phase0.Root `ssz-size:"32"`
	// electra.ExecutionRequests matches the pinned Gloas spec (consensus-specs 6ebb2216c). EIP-8282
	// (builder deposit/exit requests, slated for Glamsterdam) will extend it — swap to a node-side Gloas
	// variant when the target devnet adopts it.
	ExecutionRequests     *electra.ExecutionRequests
	BuilderIndex          BuilderIndex
	BeaconBlockRoot       phase0.Root `ssz-size:"32"`
	ParentBeaconBlockRoot phase0.Root `ssz-size:"32"`
}

// Encode/Decode wrap SSZ (de)serialization — the form carried in the §6 QBFT consensus DataSSZ.
func (b *BlindedExecutionPayloadEnvelope) Encode() ([]byte, error)  { return b.MarshalSSZ() }
func (b *BlindedExecutionPayloadEnvelope) Decode(data []byte) error { return b.UnmarshalSSZ(data) }
