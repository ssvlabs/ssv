package codec_test

import (
	"encoding/json"
	"testing"

	"github.com/attestantio/go-eth2-client/api"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/holiman/uint256"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func TestMarshalUnblindBlockResponse_Deneb(t *testing.T) {
	t.Parallel()

	proposal := &api.VersionedSignedProposal{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: &deneb.BeaconBlock{
					Body: &deneb.BeaconBlockBody{
						ExecutionPayload: &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)},
					},
				},
			},
		},
	}

	resp, err := codec.MarshalUnblindBlockResponse(proposal)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if resp.Version != consensusspec.DataVersionDeneb {
		t.Fatalf("unexpected version: got %v want %v", resp.Version, consensusspec.DataVersionDeneb)
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if _, ok := decoded["version"]; !ok {
		t.Fatalf("missing top-level version")
	}
	if _, ok := decoded["data"]; !ok {
		t.Fatalf("missing top-level data")
	}
}
