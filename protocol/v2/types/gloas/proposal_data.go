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

// ValidateListLimits is ssv-spec's gloas.BeaconBlock.ValidateListLimits for the node's block type: it checks each
// of the block's lists against its Gloas limit. The lists are progressive (EIP-7916), so decoding bounds none, and
// the beacon chain rejects a block over any of them: a cluster that signed one would lose the slot.
func (d *GloasProposalData) ValidateListLimits() error {
	if d.Block == nil || d.Block.Body == nil {
		return fmt.Errorf("nil block body")
	}
	body := d.Block.Body
	type listLimit struct {
		name       string
		len, limit int
	}
	lists := []listLimit{
		{"proposer slashings", len(body.ProposerSlashings), specgloas.MaxProposerSlashings},
		{"attester slashings", len(body.AttesterSlashings), specgloas.MaxAttesterSlashings},
		{"attestations", len(body.Attestations), specgloas.MaxAttestations},
		{"deposits", len(body.Deposits), specgloas.MaxDeposits},
		{"voluntary exits", len(body.VoluntaryExits), specgloas.MaxVoluntaryExits},
		{"BLS to execution changes", len(body.BLSToExecutionChanges), specgloas.MaxBLSToExecutionChanges},
		{"payload attestations", len(body.PayloadAttestations), specgloas.MaxPayloadAttestations},
	}
	if requests := body.ParentExecutionRequests; requests != nil {
		lists = append(lists,
			listLimit{"withdrawal requests", len(requests.Withdrawals), specgloas.MaxWithdrawalRequestsPerPayload},
			listLimit{"consolidation requests", len(requests.Consolidations), specgloas.MaxConsolidationRequestsPerPayload},
			listLimit{"builder deposit requests", len(requests.BuilderDeposits), specgloas.MaxBuilderDepositRequestsPerPayload},
			listLimit{"builder exit requests", len(requests.BuilderExits), specgloas.MaxBuilderExitRequestsPerPayload},
		)
	}
	if bid := body.SignedExecutionPayloadBid; bid != nil && bid.Message != nil {
		lists = append(lists, listLimit{"blob KZG commitments", len(bid.Message.BlobKZGCommitments), specgloas.MaxBlobCommitmentsPerBlock})
	}
	for _, list := range lists {
		if list.len > list.limit {
			return fmt.Errorf("%d %s, over the limit of %d", list.len, list.name, list.limit)
		}
	}
	return nil
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
