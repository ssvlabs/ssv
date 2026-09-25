package gloas

import (
	apiv1gloas "github.com/attestantio/go-eth2-client/api/v1/gloas"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// BlockContents is the produceBlockV4 self-build response body (beacon-APIs Gloas.BlockContents, returned
// for include_payload=true): the block plus everything the proposer reveals in §6 — the execution-payload
// envelope, the blobs, and their KZG cell proofs. Holding them from the produce call on means no
// beacon-node call sits between the §4 decision and the reveal, and the reveal never depends on which
// beacon node built the block (SIP #94 §6).
type BlockContents = apiv1gloas.BlockContents

// SignedExecutionPayloadEnvelopeContents is the §6 publish body (beacon-APIs
// Gloas.SignedExecutionPayloadEnvelopeContents, sent with Eth-Blob-Data-Included: true): the signed envelope
// with its blobs and KZG cell proofs, so any beacon node can broadcast the reveal, not only the one that
// built the block (SIP #94 §6).
type SignedExecutionPayloadEnvelopeContents = apiv1gloas.SignedExecutionPayloadEnvelopeContents

// ProducedBlock is one produceBlockV4 response: the block, the winning builder's URL (empty when self-built
// or won over p2p), and, for a self-build, the envelope contents the operator reveals in §6.
type ProducedBlock struct {
	Block      *BeaconBlock
	BuilderURL string
	// Envelope is set only when the beacon node self-built the block and answered with BlockContents.
	Envelope *ProducedEnvelope
}

// ProducedEnvelope is a self-build envelope with its blobs and KZG cell proofs, held by the builder operator
// from the produce call until the §6 reveal.
type ProducedEnvelope struct {
	Envelope  *ExecutionPayloadEnvelope
	KZGProofs []deneb.KZGProof
	Blobs     []deneb.Blob
}

// Signed wraps the envelope and the reconstructed threshold signature into the §6 publish body.
func (p *ProducedEnvelope) Signed(signature phase0.BLSSignature) *SignedExecutionPayloadEnvelopeContents {
	return &SignedExecutionPayloadEnvelopeContents{
		SignedExecutionPayloadEnvelope: &SignedExecutionPayloadEnvelope{Message: p.Envelope, Signature: signature},
		KZGProofs:                      p.KZGProofs,
		Blobs:                          p.Blobs,
	}
}
