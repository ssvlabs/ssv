package gloas

import (
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Regenerate with `go generate ./...`. The includes are resolved from the module graph (`go list -m`)
// so they track go-eth2-client across dependency bumps rather than pinning.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path ./execution_payload_envelope.go --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/electra,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/bellatrix --objs BlindedExecutionPayloadEnvelope"

// BlindedExecutionPayloadEnvelope is the blinded form of the Gloas ExecutionPayloadEnvelope that the §6
// envelope-signing duty signs (SIP #94 §6): the full `payload` is replaced by
// PayloadRoot = hash_tree_root(payload). By SSZ Container positional merkleization its hash-tree root
// equals the full envelope's, so a BLS signature over the blinded signing root is valid for the full
// SignedExecutionPayloadEnvelope. It rides in EnvelopeConsensusData.DataSSZ; blinding keeps that QBFT
// value bounded — a few hundred bytes rather than the full payload's hundreds of KB to ~MB.
type BlindedExecutionPayloadEnvelope struct {
	PayloadRoot           phase0.Root `ssz-size:"32"`
	ExecutionRequests     *electra.ExecutionRequests
	BuilderIndex          uint64
	BeaconBlockRoot       phase0.Root `ssz-size:"32"`
	ParentBeaconBlockRoot phase0.Root `ssz-size:"32"`
}

// Encode/Decode wrap SSZ (de)serialization — the form carried in the §6 QBFT consensus DataSSZ.
func (b *BlindedExecutionPayloadEnvelope) Encode() ([]byte, error)  { return b.MarshalSSZ() }
func (b *BlindedExecutionPayloadEnvelope) Decode(data []byte) error { return b.UnmarshalSSZ(data) }
