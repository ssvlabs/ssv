package codec_test

import (
	"bytes"
	"testing"

	builderdeneb "github.com/attestantio/go-builder-client/api/deneb"
	eth2api "github.com/attestantio/go-eth2-client/api"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/holiman/uint256"

	buildercodec "github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func TestBuildSubmitBlindedBlockResponseJSON_Bellatrix(t *testing.T) {
	t.Parallel()

	proposal := &eth2api.VersionedSignedProposal{
		Version: consensusspec.DataVersionBellatrix,
		Bellatrix: &bellatrix.SignedBeaconBlock{
			Message: &bellatrix.BeaconBlock{
				Body: &bellatrix.BeaconBlockBody{
					ExecutionPayload: &bellatrix.ExecutionPayload{Transactions: []bellatrix.Transaction{}},
				},
			},
		},
	}

	resp, err := buildercodec.BuildSubmitBlindedBlockResponseJSON(proposal)
	if err != nil {
		t.Fatalf("build response: %v", err)
	}
	if resp.Version != consensusspec.DataVersionBellatrix {
		t.Fatalf("unexpected version: got %v want %v", resp.Version, consensusspec.DataVersionBellatrix)
	}
	if resp.Bellatrix == nil {
		t.Fatalf("expected bellatrix response payload")
	}
	if resp.IsEmpty() {
		t.Fatalf("unexpected empty response")
	}
}

func TestBuildSubmitBlindedBlockResponseJSON_Capella(t *testing.T) {
	t.Parallel()

	proposal := &eth2api.VersionedSignedProposal{
		Version: consensusspec.DataVersionCapella,
		Capella: &capella.SignedBeaconBlock{
			Message: &capella.BeaconBlock{
				Body: &capella.BeaconBlockBody{
					ExecutionPayload: &capella.ExecutionPayload{Transactions: []bellatrix.Transaction{}},
				},
			},
		},
	}

	resp, err := buildercodec.BuildSubmitBlindedBlockResponseJSON(proposal)
	if err != nil {
		t.Fatalf("build response: %v", err)
	}
	if resp.Version != consensusspec.DataVersionCapella {
		t.Fatalf("unexpected version: got %v want %v", resp.Version, consensusspec.DataVersionCapella)
	}
	if resp.Capella == nil {
		t.Fatalf("expected capella response payload")
	}
	if resp.IsEmpty() {
		t.Fatalf("unexpected empty response")
	}
}

func TestBuildSubmitBlindedBlockResponseJSON_Deneb(t *testing.T) {
	t.Parallel()

	proposal := &eth2api.VersionedSignedProposal{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: &deneb.BeaconBlock{
					Body: &deneb.BeaconBlockBody{
						ExecutionPayload:   &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)},
						BlobKZGCommitments: []deneb.KZGCommitment{},
					},
				},
			},
			KZGProofs: []deneb.KZGProof{},
			Blobs:     []deneb.Blob{},
		},
	}

	resp, err := buildercodec.BuildSubmitBlindedBlockResponseJSON(proposal)
	if err != nil {
		t.Fatalf("build response: %v", err)
	}
	if resp.Version != consensusspec.DataVersionDeneb {
		t.Fatalf("unexpected version: got %v want %v", resp.Version, consensusspec.DataVersionDeneb)
	}
	if resp.Deneb == nil {
		t.Fatalf("expected deneb response payload bundle")
	}
	if resp.IsEmpty() {
		t.Fatalf("unexpected empty response")
	}
}

func TestMarshalSubmitBlindedBlockResponseSSZ_Bellatrix_MatchesPayload(t *testing.T) {
	t.Parallel()

	payload := &bellatrix.ExecutionPayload{Transactions: []bellatrix.Transaction{}}
	proposal := &eth2api.VersionedSignedProposal{
		Version: consensusspec.DataVersionBellatrix,
		Bellatrix: &bellatrix.SignedBeaconBlock{
			Message: &bellatrix.BeaconBlock{
				Body: &bellatrix.BeaconBlockBody{
					ExecutionPayload: payload,
				},
			},
		},
	}

	want, err := payload.MarshalSSZ()
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	got, err := buildercodec.MarshalSubmitBlindedBlockResponseSSZ(proposal)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected ssz bytes")
	}
}

func TestMarshalSubmitBlindedBlockResponseSSZ_Capella_MatchesPayload(t *testing.T) {
	t.Parallel()

	payload := &capella.ExecutionPayload{Transactions: []bellatrix.Transaction{}}
	proposal := &eth2api.VersionedSignedProposal{
		Version: consensusspec.DataVersionCapella,
		Capella: &capella.SignedBeaconBlock{
			Message: &capella.BeaconBlock{
				Body: &capella.BeaconBlockBody{
					ExecutionPayload: payload,
				},
			},
		},
	}

	want, err := payload.MarshalSSZ()
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	got, err := buildercodec.MarshalSubmitBlindedBlockResponseSSZ(proposal)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected ssz bytes")
	}
}

func TestMarshalSubmitBlindedBlockResponseSSZ_Deneb_MatchesBundle(t *testing.T) {
	t.Parallel()

	payload := &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)}
	proposal := &eth2api.VersionedSignedProposal{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: &deneb.BeaconBlock{
					Body: &deneb.BeaconBlockBody{
						ExecutionPayload:   payload,
						BlobKZGCommitments: []deneb.KZGCommitment{},
					},
				},
			},
			KZGProofs: []deneb.KZGProof{},
			Blobs:     []deneb.Blob{},
		},
	}

	wantBundle := &builderdeneb.ExecutionPayloadAndBlobsBundle{
		ExecutionPayload: payload,
		BlobsBundle: &builderdeneb.BlobsBundle{
			Commitments: []deneb.KZGCommitment{},
			Proofs:      []deneb.KZGProof{},
			Blobs:       []deneb.Blob{},
		},
	}
	want, err := wantBundle.MarshalSSZ()
	if err != nil {
		t.Fatalf("marshal expected bundle: %v", err)
	}

	got, err := buildercodec.MarshalSubmitBlindedBlockResponseSSZ(proposal)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected ssz bytes")
	}
}
