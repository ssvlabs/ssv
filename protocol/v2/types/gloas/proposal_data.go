package gloas

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	specgloas "github.com/ssvlabs/ssv-spec/types/gloas"
)

// GloasProposalData is the value the SSV cluster decides at Gloas slots (SIP #94 §4): the beacon block plus
// the proposer's own self-build payload_root — hash_tree_root(envelope.payload), the one envelope field the
// block does not commit to. Carrying it in the decided value lets every operator derive the §6 blinded
// envelope (DeriveBlindedEnvelope) and sign its root as the second entry of the block's post-consensus
// packet, so the reveal needs no round of its own. Zero for an external bid. This is ssv-spec's DataSSZ
// wrapper, byte-identical on the wire, built here on go-eth2-client's block type.
type GloasProposalData struct {
	Block       *BeaconBlock
	PayloadRoot phase0.Root `ssz-size:"32"`
}

func (d *GloasProposalData) Encode() ([]byte, error)  { return d.MarshalSSZ() }
func (d *GloasProposalData) Decode(data []byte) error { return d.UnmarshalSSZ(data) }

// DecodeGloasProposalData unmarshals the Gloas DataSSZ wrapper of a decided proposer value. SSZ decoding
// populates every nested container, so the block's bid — which SelfBuild and DeriveBlindedEnvelope read —
// is always present on a decoded value.
func DecodeGloasProposalData(dataSSZ []byte) (*GloasProposalData, error) {
	d := &GloasProposalData{}
	if err := d.UnmarshalSSZ(dataSSZ); err != nil {
		return nil, err
	}
	return d, nil
}

// SelfBuild reports whether the decided block commits to a self-built payload (BUILDER_INDEX_SELF_BUILD)
// rather than an external builder's bid (SIP #94 §4).
func (d *GloasProposalData) SelfBuild() bool {
	return d.Block.Body.SignedExecutionPayloadBid.Message.BuilderIndex == BuilderIndexSelfBuild
}

// DeriveBlindedEnvelope builds the §6 blinded envelope from the decided value alone (SIP #94 §6): the
// decided payload_root, the requests root the bid commits to, the self-build builder index, the block's
// root and its parent root. Its progressive root is the full envelope's, so a threshold signature over it
// is valid for the reveal. Only meaningful for a self-build value.
func (d *GloasProposalData) DeriveBlindedEnvelope() (*BlindedExecutionPayloadEnvelope, error) {
	blockRoot, err := d.Block.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("hash tree root of decided gloas block: %w", err)
	}
	return &BlindedExecutionPayloadEnvelope{
		PayloadRoot:           d.PayloadRoot,
		ExecutionRequestsRoot: d.Block.Body.SignedExecutionPayloadBid.Message.ExecutionRequestsRoot,
		BuilderIndex:          specgloas.BuilderIndexSelfBuild,
		BeaconBlockRoot:       blockRoot,
		ParentBeaconBlockRoot: d.Block.ParentRoot,
	}, nil
}
