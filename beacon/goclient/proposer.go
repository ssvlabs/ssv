package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"go.uber.org/zap"
)

// ProposerDuties returns proposer duties for the given epoch.
func (gc *GoClient) ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
	start := time.Now()
	resp, err := gc.multiClient.ProposerDuties(ctx, &api.ProposerDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	recordRequestDuration(gc.ctx, "ProposerDuties", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)

	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "ProposerDuties"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to obtain proposer duties: %w", err)
	}
	if resp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "ProposerDuties"),
		)
		return nil, fmt.Errorf("proposer duties response is nil")
	}

	return resp.Data, nil
}

// GetBeaconBlock returns beacon block by the given slot, graffiti, and randao.
func (gc *GoClient) GetBeaconBlock(slot phase0.Slot, graffitiBytes, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	graffiti := [32]byte{}
	copy(graffiti[:], graffitiBytes[:])

	reqStart := time.Now()
	proposalResp, err := gc.multiClient.Proposal(gc.ctx, &api.ProposalOpts{
		Slot:                   slot,
		RandaoReveal:           sig,
		Graffiti:               graffiti,
		SkipRandaoVerification: false,
	})
	recordRequestDuration(gc.ctx, "Proposal", gc.multiClient.Address(), http.MethodGet, time.Since(reqStart), err)

	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "Proposal"),
			zap.Error(err),
		)
		return nil, DataVersionNil, fmt.Errorf("failed to get proposal: %w", err)
	}
	if proposalResp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "Proposal"),
		)
		return nil, DataVersionNil, fmt.Errorf("proposal response is nil")
	}
	if proposalResp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg,
			zap.String("api", "Proposal"),
		)
		return nil, DataVersionNil, fmt.Errorf("proposal data is nil")
	}

	beaconBlock := proposalResp.Data

	if beaconBlock.Blinded {
		switch beaconBlock.Version {
		case spec.DataVersionCapella:
			if beaconBlock.CapellaBlinded == nil {
				return nil, DataVersionNil, fmt.Errorf("capella blinded block is nil")
			}
			if beaconBlock.CapellaBlinded.Body == nil {
				return nil, DataVersionNil, fmt.Errorf("capella blinded block body is nil")
			}
			if beaconBlock.CapellaBlinded.Body.ExecutionPayloadHeader == nil {
				return nil, DataVersionNil, fmt.Errorf("capella blinded block execution payload header is nil")
			}
			return beaconBlock.CapellaBlinded, beaconBlock.Version, nil
		case spec.DataVersionDeneb:
			if beaconBlock.DenebBlinded == nil {
				return nil, DataVersionNil, fmt.Errorf("deneb blinded block contents is nil")
			}
			if beaconBlock.DenebBlinded.Body == nil {
				return nil, DataVersionNil, fmt.Errorf("deneb blinded block body is nil")
			}
			if beaconBlock.DenebBlinded.Body.ExecutionPayloadHeader == nil {
				return nil, DataVersionNil, fmt.Errorf("deneb blinded block execution payload header is nil")
			}
			return beaconBlock.DenebBlinded, beaconBlock.Version, nil
		case spec.DataVersionElectra:
			if beaconBlock.ElectraBlinded == nil {
				return nil, DataVersionNil, fmt.Errorf("electra blinded block is nil")
			}
			if beaconBlock.ElectraBlinded.Body == nil {
				return nil, DataVersionNil, fmt.Errorf("electra blinded block body is nil")
			}
			if beaconBlock.ElectraBlinded.Body.ExecutionPayloadHeader == nil {
				return nil, DataVersionNil, fmt.Errorf("electra blinded block execution payload header is nil")
			}
			return beaconBlock.ElectraBlinded, beaconBlock.Version, nil
		default:
			return nil, DataVersionNil, fmt.Errorf("beacon blinded block version %s not supported", beaconBlock.Version)
		}
	}

	switch beaconBlock.Version {
	case spec.DataVersionCapella:
		if beaconBlock.Capella == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block is nil")
		}
		if beaconBlock.Capella.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block body is nil")
		}
		if beaconBlock.Capella.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block execution payload is nil")
		}
		return beaconBlock.Capella, beaconBlock.Version, nil
	case spec.DataVersionDeneb:
		if beaconBlock.Deneb == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block contents is nil")
		}
		if beaconBlock.Deneb.Block == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block is nil")
		}
		if beaconBlock.Deneb.Block.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block body is nil")
		}
		if beaconBlock.Deneb.Block.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block execution payload is nil")
		}
		return beaconBlock.Deneb, beaconBlock.Version, nil
	case spec.DataVersionElectra:
		if beaconBlock.Electra == nil {
			return nil, DataVersionNil, fmt.Errorf("electra block contents is nil")
		}
		if beaconBlock.Electra.Block == nil {
			return nil, DataVersionNil, fmt.Errorf("electra block is nil")
		}
		if beaconBlock.Electra.Block.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("electra block body is nil")
		}
		if beaconBlock.Electra.Block.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("electra block execution payload is nil")
		}
		return beaconBlock.Electra, beaconBlock.Version, nil
	default:
		return nil, DataVersionNil, fmt.Errorf("beacon block version %s not supported", beaconBlock.Version)
	}
}

func (gc *GoClient) SubmitBlindedBeaconBlock(block *api.VersionedBlindedProposal, sig phase0.BLSSignature) error {
	signedBlock := &api.VersionedSignedBlindedProposal{
		Version: block.Version,
	}
	switch block.Version {
	case spec.DataVersionCapella:
		if block.Capella == nil {
			return fmt.Errorf("capella blinded block is nil")
		}
		signedBlock.Capella = &apiv1capella.SignedBlindedBeaconBlock{
			Message: block.Capella,
		}
		copy(signedBlock.Capella.Signature[:], sig[:])
	case spec.DataVersionDeneb:
		if block.Deneb == nil {
			return fmt.Errorf("deneb block contents is nil")
		}
		if block.Deneb.Body == nil {
			return fmt.Errorf("deneb block body is nil")
		}
		if block.Deneb.Body.ExecutionPayloadHeader == nil {
			return fmt.Errorf("deneb block execution payload header is nil")
		}
		signedBlock.Deneb = &apiv1deneb.SignedBlindedBeaconBlock{
			Message: block.Deneb,
		}
		copy(signedBlock.Deneb.Signature[:], sig[:])
	case spec.DataVersionElectra:
		if block.Electra == nil {
			return fmt.Errorf("electra block contents is nil")
		}
		if block.Electra.Body == nil {
			return fmt.Errorf("electra block body is nil")
		}
		if block.Electra.Body.ExecutionPayloadHeader == nil {
			return fmt.Errorf("electra block execution payload header is nil")
		}
		signedBlock.Electra = &apiv1electra.SignedBlindedBeaconBlock{
			Message: block.Electra,
		}
		copy(signedBlock.Electra.Signature[:], sig[:])
	default:
		return fmt.Errorf("unknown block version")
	}

	opts := &api.SubmitBlindedProposalOpts{
		Proposal: signedBlock,
	}

	return gc.multiClientSubmit("SubmitBlindedProposal", func(ctx context.Context, client Client) error {
		return client.SubmitBlindedProposal(ctx, opts)
	})
}

// SubmitBeaconBlock submit the block to the node
func (gc *GoClient) SubmitBeaconBlock(block *api.VersionedProposal, sig phase0.BLSSignature) error {
	signedBlock := &api.VersionedSignedProposal{
		Version: block.Version,
	}
	switch block.Version {
	case spec.DataVersionCapella:
		if block.Capella == nil {
			return fmt.Errorf("capella block is nil")
		}
		signedBlock.Capella = &capella.SignedBeaconBlock{
			Message: block.Capella,
		}
		copy(signedBlock.Capella.Signature[:], sig[:])
	case spec.DataVersionDeneb:
		if block.Deneb == nil {
			return fmt.Errorf("deneb block contents is nil")
		}
		if block.Deneb.Block == nil {
			return fmt.Errorf("deneb block is nil")
		}
		if block.Deneb.Block.Body == nil {
			return fmt.Errorf("deneb block body is nil")
		}
		if block.Deneb.Block.Body.ExecutionPayload == nil {
			return fmt.Errorf("deneb block execution payload header is nil")
		}
		signedBlock.Deneb = &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: block.Deneb.Block,
			},
			KZGProofs: block.Deneb.KZGProofs,
			Blobs:     block.Deneb.Blobs,
		}
		copy(signedBlock.Deneb.SignedBlock.Signature[:], sig[:])
	case spec.DataVersionElectra:
		if block.Electra == nil {
			return fmt.Errorf("electra block contents is nil")
		}
		if block.Electra.Block == nil {
			return fmt.Errorf("electra block is nil")
		}
		if block.Electra.Block.Body == nil {
			return fmt.Errorf("electra block body is nil")
		}
		if block.Electra.Block.Body.ExecutionPayload == nil {
			return fmt.Errorf("electra block execution payload header is nil")
		}
		signedBlock.Electra = &apiv1electra.SignedBlockContents{
			SignedBlock: &electra.SignedBeaconBlock{
				Message: block.Electra.Block,
			},
			KZGProofs: block.Electra.KZGProofs,
			Blobs:     block.Electra.Blobs,
		}
		copy(signedBlock.Electra.SignedBlock.Signature[:], sig[:])
	default:
		return fmt.Errorf("unknown block version")
	}

	opts := &api.SubmitProposalOpts{
		Proposal: signedBlock,
	}

	return gc.multiClientSubmit("SubmitProposal", func(ctx context.Context, client Client) error {
		return client.SubmitProposal(gc.ctx, opts)
	})
}

func (gc *GoClient) SubmitProposalPreparation(feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
	var preparations []*eth2apiv1.ProposalPreparation
	for index, recipient := range feeRecipients {
		preparations = append(preparations, &eth2apiv1.ProposalPreparation{
			ValidatorIndex: index,
			FeeRecipient:   recipient,
		})
	}

	return gc.multiClientSubmit("SubmitProposalPreparations", func(ctx context.Context, client Client) error {
		return client.SubmitProposalPreparations(ctx, preparations)
	})
}
