package codec

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/deneb"
)

type UnblindBlockResponse struct {
	Version spec.DataVersion          `json:"version"`
	Data    *UnblindBlockResponseData `json:"data"`
}

type UnblindBlockResponseData struct {
	ExecutionPayload *deneb.ExecutionPayload          `json:"execution_payload"`
	BlobsBundle      *UnblindBlockResponseBlobsBundle `json:"blobs_bundle"`
}

type UnblindBlockResponseBlobsBundle struct {
	Commitments []deneb.KZGCommitment `json:"commitments"`
	Proofs      []deneb.KZGProof      `json:"proofs"`
	Blobs       []deneb.Blob          `json:"blobs"`
}

// MarshalUnblindBlockResponse creates the v1 builder endpoint JSON response envelope for an unblinded proposal.
func MarshalUnblindBlockResponse(proposal *api.VersionedSignedProposal) (*UnblindBlockResponse, error) {
	if proposal == nil {
		return nil, fmt.Errorf("nil proposal")
	}

	resp := &UnblindBlockResponse{
		Version: proposal.Version,
	}

	switch proposal.Version {
	case spec.DataVersionDeneb:
		if proposal.Deneb == nil || proposal.Deneb.SignedBlock == nil || proposal.Deneb.SignedBlock.Message == nil || proposal.Deneb.SignedBlock.Message.Body == nil {
			return nil, fmt.Errorf("missing deneb proposal data")
		}
		resp.Data = &UnblindBlockResponseData{
			ExecutionPayload: proposal.Deneb.SignedBlock.Message.Body.ExecutionPayload,
			BlobsBundle: &UnblindBlockResponseBlobsBundle{
				Commitments: proposal.Deneb.SignedBlock.Message.Body.BlobKZGCommitments,
				Proofs:      proposal.Deneb.KZGProofs,
				Blobs:       proposal.Deneb.Blobs,
			},
		}
	case spec.DataVersionElectra:
		if proposal.Electra == nil || proposal.Electra.SignedBlock == nil || proposal.Electra.SignedBlock.Message == nil || proposal.Electra.SignedBlock.Message.Body == nil {
			return nil, fmt.Errorf("missing electra proposal data")
		}
		resp.Data = &UnblindBlockResponseData{
			ExecutionPayload: proposal.Electra.SignedBlock.Message.Body.ExecutionPayload,
			BlobsBundle: &UnblindBlockResponseBlobsBundle{
				Commitments: proposal.Electra.SignedBlock.Message.Body.BlobKZGCommitments,
				Proofs:      proposal.Electra.KZGProofs,
				Blobs:       proposal.Electra.Blobs,
			},
		}
	case spec.DataVersionFulu:
		if proposal.Fulu == nil || proposal.Fulu.SignedBlock == nil || proposal.Fulu.SignedBlock.Message == nil || proposal.Fulu.SignedBlock.Message.Body == nil {
			return nil, fmt.Errorf("missing fulu proposal data")
		}
		resp.Data = &UnblindBlockResponseData{
			ExecutionPayload: proposal.Fulu.SignedBlock.Message.Body.ExecutionPayload,
			BlobsBundle: &UnblindBlockResponseBlobsBundle{
				Commitments: proposal.Fulu.SignedBlock.Message.Body.BlobKZGCommitments,
				Proofs:      proposal.Fulu.KZGProofs,
				Blobs:       proposal.Fulu.Blobs,
			},
		}
	default:
		return nil, fmt.Errorf("unsupported version %v", proposal.Version)
	}

	if resp.Data.ExecutionPayload == nil {
		return nil, fmt.Errorf("missing execution payload")
	}
	if resp.Data.BlobsBundle == nil {
		return nil, fmt.Errorf("missing blobs bundle")
	}

	return resp, nil
}
