package gloas

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Regenerate with `go generate ./...`. -path is the package dir (not just this file) so sszgen resolves
// the sibling gloas BuilderIndex the envelope references; --objs limits output to the envelope types,
// collected into its own _encoding.go. Includes track go-eth2-client via `go list -m`.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path . --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/electra,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/bellatrix,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/capella --objs BlindedExecutionPayloadEnvelope,ExecutionPayloadEnvelope,SignedExecutionPayloadEnvelope --exclude-objs ExecutionPayload,ExecutionRequests,BuilderDepositRequest,BuilderExitRequest --output ./execution_payload_envelope_encoding.go"

// BlindedExecutionPayloadEnvelope is the blinded form of the Gloas ExecutionPayloadEnvelope that the §6
// envelope-signing duty signs (SIP #94 §6): the full `payload` is replaced by
// PayloadRoot = hash_tree_root(payload). By SSZ Container positional merkleization its hash-tree root
// equals the full envelope's, so a BLS signature over the blinded signing root is valid for the full
// SignedExecutionPayloadEnvelope. It rides in EnvelopeConsensusData.DataSSZ; blinding keeps that QBFT
// value bounded — a few hundred bytes rather than the full payload's hundreds of KB to ~MB.
//
// It is an SSV-internal consensus type only, never sent on the wire: beacon-APIs#624 removed the
// identically named spec container, and §6 publishes the full SignedExecutionPayloadEnvelope.
type BlindedExecutionPayloadEnvelope struct {
	PayloadRoot phase0.Root `ssz-size:"32"`
	// Gloas execution requests — the EIP-8282 five-list variant, not electra's three (see execution_requests.go).
	ExecutionRequests     *ExecutionRequests
	BuilderIndex          BuilderIndex
	BeaconBlockRoot       phase0.Root `ssz-size:"32"`
	ParentBeaconBlockRoot phase0.Root `ssz-size:"32"`
}

// Encode/Decode wrap SSZ (de)serialization — the form carried in the §6 QBFT consensus DataSSZ.
func (b *BlindedExecutionPayloadEnvelope) Encode() ([]byte, error)  { return b.MarshalSSZ() }
func (b *BlindedExecutionPayloadEnvelope) Decode(data []byte) error { return b.UnmarshalSSZ(data) }

// ExecutionPayloadEnvelope is the full (unblinded) Gloas execution-payload envelope (SIP #94 §6). The §6
// duty signs the blinded form above; this is the body the builder publishes once the cluster reconstructs
// the signature. Its hash-tree root equals the blinded envelope's when PayloadRoot = hash_tree_root(Payload),
// so the signature over the blinded root is valid for this full envelope.
type ExecutionPayloadEnvelope struct {
	Payload               *ExecutionPayload
	ExecutionRequests     *ExecutionRequests
	BuilderIndex          BuilderIndex
	BeaconBlockRoot       phase0.Root `ssz-size:"32"`
	ParentBeaconBlockRoot phase0.Root `ssz-size:"32"`
}

// SignedExecutionPayloadEnvelope wraps the envelope with the builder's signature (under
// DOMAIN_BEACON_BUILDER). The cluster reconstructs this full signed form and publishes it as-is (§6);
// beacon-APIs#624 removed the blinded publication body, so the only deferred alternative is the stateless
// SignedExecutionPayloadEnvelopeContents (full envelope + blobs/KZG — not yet wired).
type SignedExecutionPayloadEnvelope struct {
	Message   *ExecutionPayloadEnvelope
	Signature phase0.BLSSignature `ssz-size:"96"`
}

// Blinded returns the blinded form of the envelope — the full Payload replaced by its hash-tree root.
// The blinded envelope hashes to the same root as this one, so the §6 duty agrees on and signs the
// blinded value while the signature stays valid for this full envelope. The non-Payload fields are
// shared (not copied), so the result must not outlive this envelope.
func (e *ExecutionPayloadEnvelope) Blinded() (*BlindedExecutionPayloadEnvelope, error) {
	payloadRoot, err := e.Payload.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("hash tree root of execution payload: %w", err)
	}
	return &BlindedExecutionPayloadEnvelope{
		PayloadRoot:           payloadRoot,
		ExecutionRequests:     e.ExecutionRequests,
		BuilderIndex:          e.BuilderIndex,
		BeaconBlockRoot:       e.BeaconBlockRoot,
		ParentBeaconBlockRoot: e.ParentBeaconBlockRoot,
	}, nil
}
