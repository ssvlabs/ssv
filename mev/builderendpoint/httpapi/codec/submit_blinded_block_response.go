package codec

import (
	"fmt"

	builderapi "github.com/attestantio/go-builder-client/api"
	builderdeneb "github.com/attestantio/go-builder-client/api/deneb"
	builderfulu "github.com/attestantio/go-builder-client/api/fulu"
	eth2api "github.com/attestantio/go-eth2-client/api"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
)

// BuildSubmitBlindedBlockResponseJSON builds the Builder API v1 JSON response payload for submitBlindedBlock.
// It matches the builder-specs schema: {version, data}, where data is either an execution payload or an
// execution payload + blobs bundle (post-Deneb).
func BuildSubmitBlindedBlockResponseJSON(proposal *eth2api.VersionedSignedProposal) (*builderapi.VersionedSubmitBlindedBlockResponse, error) {
	if proposal == nil {
		return nil, fmt.Errorf("nil proposal")
	}

	resp := &builderapi.VersionedSubmitBlindedBlockResponse{
		Version: proposal.Version,
	}

	switch proposal.Version {
	case consensusspec.DataVersionBellatrix:
		if proposal.Bellatrix == nil || proposal.Bellatrix.Message == nil || proposal.Bellatrix.Message.Body == nil {
			return nil, fmt.Errorf("missing bellatrix proposal data")
		}
		resp.Bellatrix = proposal.Bellatrix.Message.Body.ExecutionPayload
	case consensusspec.DataVersionCapella:
		if proposal.Capella == nil || proposal.Capella.Message == nil || proposal.Capella.Message.Body == nil {
			return nil, fmt.Errorf("missing capella proposal data")
		}
		resp.Capella = proposal.Capella.Message.Body.ExecutionPayload
	case consensusspec.DataVersionDeneb:
		bundle, err := denebBundleFromProposal(proposal)
		if err != nil {
			return nil, err
		}
		resp.Deneb = bundle
	case consensusspec.DataVersionElectra:
		bundle, err := denebBundleFromProposal(proposal)
		if err != nil {
			return nil, err
		}
		resp.Electra = bundle
	case consensusspec.DataVersionFulu:
		bundle, err := fuluBundleFromProposal(proposal)
		if err != nil {
			return nil, err
		}
		resp.Fulu = bundle
	default:
		return nil, fmt.Errorf("unsupported version %v", proposal.Version)
	}

	if resp.IsEmpty() {
		return nil, fmt.Errorf("missing response data")
	}

	return resp, nil
}

// MarshalSubmitBlindedBlockResponseSSZ marshals the Builder API v1 response as SSZ bytes.
// Per builder-specs this is the SSZ encoding of the inner `ExecutionPayload` or
// `ExecutionPayloadAndBlobsBundle` (not the versioned JSON envelope).
func MarshalSubmitBlindedBlockResponseSSZ(proposal *eth2api.VersionedSignedProposal) ([]byte, error) {
	if proposal == nil {
		return nil, fmt.Errorf("nil proposal")
	}

	switch proposal.Version {
	case consensusspec.DataVersionBellatrix:
		if proposal.Bellatrix == nil || proposal.Bellatrix.Message == nil || proposal.Bellatrix.Message.Body == nil || proposal.Bellatrix.Message.Body.ExecutionPayload == nil {
			return nil, fmt.Errorf("missing bellatrix execution payload")
		}
		return proposal.Bellatrix.Message.Body.ExecutionPayload.MarshalSSZ()
	case consensusspec.DataVersionCapella:
		if proposal.Capella == nil || proposal.Capella.Message == nil || proposal.Capella.Message.Body == nil || proposal.Capella.Message.Body.ExecutionPayload == nil {
			return nil, fmt.Errorf("missing capella execution payload")
		}
		return proposal.Capella.Message.Body.ExecutionPayload.MarshalSSZ()
	case consensusspec.DataVersionDeneb, consensusspec.DataVersionElectra:
		bundle, err := denebBundleFromProposal(proposal)
		if err != nil {
			return nil, err
		}
		return bundle.MarshalSSZ()
	case consensusspec.DataVersionFulu:
		bundle, err := fuluBundleFromProposal(proposal)
		if err != nil {
			return nil, err
		}
		return bundle.MarshalSSZ()
	default:
		return nil, fmt.Errorf("unsupported version %v", proposal.Version)
	}
}

func denebBundleFromProposal(proposal *eth2api.VersionedSignedProposal) (*builderdeneb.ExecutionPayloadAndBlobsBundle, error) {
	if proposal == nil {
		return nil, fmt.Errorf("nil proposal")
	}

	switch proposal.Version {
	case consensusspec.DataVersionDeneb:
		if proposal.Deneb == nil || proposal.Deneb.SignedBlock == nil || proposal.Deneb.SignedBlock.Message == nil || proposal.Deneb.SignedBlock.Message.Body == nil {
			return nil, fmt.Errorf("missing deneb proposal data")
		}
		return &builderdeneb.ExecutionPayloadAndBlobsBundle{
			ExecutionPayload: proposal.Deneb.SignedBlock.Message.Body.ExecutionPayload,
			BlobsBundle: &builderdeneb.BlobsBundle{
				Commitments: proposal.Deneb.SignedBlock.Message.Body.BlobKZGCommitments,
				Proofs:      proposal.Deneb.KZGProofs,
				Blobs:       proposal.Deneb.Blobs,
			},
		}, nil
	case consensusspec.DataVersionElectra:
		if proposal.Electra == nil || proposal.Electra.SignedBlock == nil || proposal.Electra.SignedBlock.Message == nil || proposal.Electra.SignedBlock.Message.Body == nil {
			return nil, fmt.Errorf("missing electra proposal data")
		}
		return &builderdeneb.ExecutionPayloadAndBlobsBundle{
			ExecutionPayload: proposal.Electra.SignedBlock.Message.Body.ExecutionPayload,
			BlobsBundle: &builderdeneb.BlobsBundle{
				Commitments: proposal.Electra.SignedBlock.Message.Body.BlobKZGCommitments,
				Proofs:      proposal.Electra.KZGProofs,
				Blobs:       proposal.Electra.Blobs,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported version for deneb bundle %v", proposal.Version)
	}
}

func fuluBundleFromProposal(proposal *eth2api.VersionedSignedProposal) (*builderfulu.ExecutionPayloadAndBlobsBundle, error) {
	if proposal == nil {
		return nil, fmt.Errorf("nil proposal")
	}
	if proposal.Version != consensusspec.DataVersionFulu {
		return nil, fmt.Errorf("unsupported version for fulu bundle %v", proposal.Version)
	}
	if proposal.Fulu == nil || proposal.Fulu.SignedBlock == nil || proposal.Fulu.SignedBlock.Message == nil || proposal.Fulu.SignedBlock.Message.Body == nil {
		return nil, fmt.Errorf("missing fulu proposal data")
	}

	return &builderfulu.ExecutionPayloadAndBlobsBundle{
		ExecutionPayload: proposal.Fulu.SignedBlock.Message.Body.ExecutionPayload,
		BlobsBundle: &builderfulu.BlobsBundle{
			Commitments: proposal.Fulu.SignedBlock.Message.Body.BlobKZGCommitments,
			Proofs:      proposal.Fulu.KZGProofs,
			Blobs:       proposal.Fulu.Blobs,
		},
	}, nil
}
