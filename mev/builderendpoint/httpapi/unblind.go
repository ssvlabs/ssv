package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/attestantio/go-eth2-client/api"
	apiv1bellatrix "github.com/attestantio/go-eth2-client/api/v1/bellatrix"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/pkg/errors"
)

func (rt *Router) postBlindedBlocks(w http.ResponseWriter, r *http.Request) {
	if rt.unblinder == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ctx := r.Context()

	signedBlindedBeaconBlock, err := obtainBlindedBlock(ctx, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unable to obtain blinded block")
		return
	}

	signedProposal, err := rt.unblinder.UnblindBlock(ctx, signedBlindedBeaconBlock)
	if err != nil {
		// MVP: treat all errors as internal failures.
		writeError(w, http.StatusInternalServerError, "failed to unblind block")
		return
	}

	if signedProposal == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp, err := outputUnblindedResponse(ctx, signedProposal)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate unblinded response")
		return
	}

	w.Header().Set(EthConsensusVersion, strings.ToLower(resp.Version.String()))
	writeJSON(w, http.StatusOK, resp)
}

func obtainBlindedBlock(ctx context.Context, r *http.Request) (*api.VersionedSignedBlindedBeaconBlock, error) {
	contentType := obtainContentType(r)

	consensusVersion := r.Header.Get(EthConsensusVersion)
	if consensusVersion == "" {
		return nil, fmt.Errorf("no %s header provided", EthConsensusVersion)
	}

	return unmarshalBlindedBlock(ctx, contentType, consensusVersion, r.Body)
}

func obtainContentType(r *http.Request) string {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		// Backwards-compatible default.
		contentType = "application/json"
	}
	if idx := strings.Index(contentType, ";"); idx > 0 {
		contentType = contentType[:idx]
	}
	return strings.TrimSpace(contentType)
}

func unmarshalBlindedBlock(ctx context.Context, contentType string, consensusVersion string, body io.Reader) (*api.VersionedSignedBlindedBeaconBlock, error) {
	signedBlindedBeaconBlock := &api.VersionedSignedBlindedBeaconBlock{}

	switch strings.ToLower(consensusVersion) {
	case "bellatrix":
		signedBlindedBeaconBlock.Version = spec.DataVersionBellatrix
		signedBlindedBeaconBlock.Bellatrix = &apiv1bellatrix.SignedBlindedBeaconBlock{}
	case "capella":
		signedBlindedBeaconBlock.Version = spec.DataVersionCapella
		signedBlindedBeaconBlock.Capella = &apiv1capella.SignedBlindedBeaconBlock{}
	case "deneb":
		signedBlindedBeaconBlock.Version = spec.DataVersionDeneb
		signedBlindedBeaconBlock.Deneb = &apiv1deneb.SignedBlindedBeaconBlock{}
	case "electra":
		signedBlindedBeaconBlock.Version = spec.DataVersionElectra
		signedBlindedBeaconBlock.Electra = &apiv1electra.SignedBlindedBeaconBlock{}
	case "fulu":
		signedBlindedBeaconBlock.Version = spec.DataVersionFulu
		signedBlindedBeaconBlock.Fulu = &apiv1electra.SignedBlindedBeaconBlock{}
	default:
		return nil, fmt.Errorf("unknown block version %v", consensusVersion)
	}

	switch strings.ToLower(contentType) {
	case "application/octet-stream":
		return unmarshalBlindedBlockSSZ(ctx, signedBlindedBeaconBlock, body)
	case "application/json":
		return unmarshalBlindedBlockJSON(ctx, signedBlindedBeaconBlock, body)
	default:
		return nil, fmt.Errorf("unsupported content type %s", contentType)
	}
}

func unmarshalBlindedBlockJSON(_ context.Context, block *api.VersionedSignedBlindedBeaconBlock, body io.Reader) (*api.VersionedSignedBlindedBeaconBlock, error) {
	var err error
	switch block.Version {
	case spec.DataVersionBellatrix:
		err = json.NewDecoder(body).Decode(block.Bellatrix)
	case spec.DataVersionCapella:
		err = json.NewDecoder(body).Decode(block.Capella)
	case spec.DataVersionDeneb:
		err = json.NewDecoder(body).Decode(block.Deneb)
	case spec.DataVersionElectra:
		err = json.NewDecoder(body).Decode(block.Electra)
	case spec.DataVersionFulu:
		err = json.NewDecoder(body).Decode(block.Fulu)
	default:
		err = fmt.Errorf("unsupported block version %v", block.Version)
	}
	if err != nil {
		return nil, err
	}
	return block, nil
}

func unmarshalBlindedBlockSSZ(_ context.Context, block *api.VersionedSignedBlindedBeaconBlock, body io.Reader) (*api.VersionedSignedBlindedBeaconBlock, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read body")
	}

	switch block.Version {
	case spec.DataVersionBellatrix:
		err = block.Bellatrix.UnmarshalSSZ(data)
	case spec.DataVersionCapella:
		err = block.Capella.UnmarshalSSZ(data)
	case spec.DataVersionDeneb:
		err = block.Deneb.UnmarshalSSZ(data)
	case spec.DataVersionElectra:
		err = block.Electra.UnmarshalSSZ(data)
	case spec.DataVersionFulu:
		err = block.Fulu.UnmarshalSSZ(data)
	default:
		err = fmt.Errorf("unsupported block version %v", block.Version)
	}
	if err != nil {
		return nil, err
	}

	return block, nil
}

type unblindBlockResponse struct {
	Version spec.DataVersion          `json:"version"`
	Data    *unblindBlockResponseData `json:"data"`
}

type unblindBlockResponseData struct {
	ExecutionPayload *deneb.ExecutionPayload          `json:"execution_payload"`
	BlobsBundle      *unblindBlockResponseBlobsBundle `json:"blobs_bundle"`
}

type unblindBlockResponseBlobsBundle struct {
	Commitments []deneb.KZGCommitment `json:"commitments"`
	Proofs      []deneb.KZGProof      `json:"proofs"`
	Blobs       []deneb.Blob          `json:"blobs"`
}

func outputUnblindedResponse(_ context.Context, proposal *api.VersionedSignedProposal) (*unblindBlockResponse, error) {
	resp := &unblindBlockResponse{
		Version: proposal.Version,
	}

	switch proposal.Version {
	case spec.DataVersionDeneb:
		if proposal.Deneb == nil || proposal.Deneb.SignedBlock == nil || proposal.Deneb.SignedBlock.Message == nil || proposal.Deneb.SignedBlock.Message.Body == nil {
			return nil, fmt.Errorf("missing deneb proposal data")
		}
		resp.Data = &unblindBlockResponseData{
			ExecutionPayload: proposal.Deneb.SignedBlock.Message.Body.ExecutionPayload,
			BlobsBundle: &unblindBlockResponseBlobsBundle{
				Commitments: proposal.Deneb.SignedBlock.Message.Body.BlobKZGCommitments,
				Proofs:      proposal.Deneb.KZGProofs,
				Blobs:       proposal.Deneb.Blobs,
			},
		}
	case spec.DataVersionElectra:
		if proposal.Electra == nil || proposal.Electra.SignedBlock == nil || proposal.Electra.SignedBlock.Message == nil || proposal.Electra.SignedBlock.Message.Body == nil {
			return nil, fmt.Errorf("missing electra proposal data")
		}
		resp.Data = &unblindBlockResponseData{
			ExecutionPayload: proposal.Electra.SignedBlock.Message.Body.ExecutionPayload,
			BlobsBundle: &unblindBlockResponseBlobsBundle{
				Commitments: proposal.Electra.SignedBlock.Message.Body.BlobKZGCommitments,
				Proofs:      proposal.Electra.KZGProofs,
				Blobs:       proposal.Electra.Blobs,
			},
		}
	case spec.DataVersionFulu:
		if proposal.Fulu == nil || proposal.Fulu.SignedBlock == nil || proposal.Fulu.SignedBlock.Message == nil || proposal.Fulu.SignedBlock.Message.Body == nil {
			return nil, fmt.Errorf("missing fulu proposal data")
		}
		resp.Data = &unblindBlockResponseData{
			ExecutionPayload: proposal.Fulu.SignedBlock.Message.Body.ExecutionPayload,
			BlobsBundle: &unblindBlockResponseBlobsBundle{
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
