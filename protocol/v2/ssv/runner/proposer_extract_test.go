package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/api"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	apiv1fulu "github.com/attestantio/go-eth2-client/api/v1/fulu"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestExtractExecutionFields(t *testing.T) {
	t.Run("nil block", func(t *testing.T) {
		_, err := extractExecutionInfo(nil)
		require.Error(t, err)
	})

	t.Run("unsupported version", func(t *testing.T) {
		_, err := extractExecutionInfo(&api.VersionedProposal{Version: spec.DataVersion(999)})
		require.Error(t, err)
	})

	parent := phase0.Hash32{1}
	blockHash := phase0.Hash32{2}
	const blockNumber = uint64(123)

	tests := []struct {
		name     string
		proposal *api.VersionedProposal
	}{
		{
			name: "capella full",
			proposal: &api.VersionedProposal{
				Version: spec.DataVersionCapella,
				Capella: &capella.BeaconBlock{
					Body: &capella.BeaconBlockBody{
						ExecutionPayload: &capella.ExecutionPayload{ParentHash: parent, BlockHash: blockHash, BlockNumber: blockNumber},
					},
				},
			},
		},
		{
			name: "capella blinded",
			proposal: &api.VersionedProposal{
				Version: spec.DataVersionCapella,
				Blinded: true,
				CapellaBlinded: &apiv1capella.BlindedBeaconBlock{
					Body: &apiv1capella.BlindedBeaconBlockBody{
						ExecutionPayloadHeader: &capella.ExecutionPayloadHeader{ParentHash: parent, BlockHash: blockHash, BlockNumber: blockNumber},
					},
				},
			},
		},
		{
			name: "deneb full",
			proposal: &api.VersionedProposal{
				Version: spec.DataVersionDeneb,
				Deneb: &apiv1deneb.BlockContents{
					Block: &deneb.BeaconBlock{
						Body: &deneb.BeaconBlockBody{
							ExecutionPayload: &deneb.ExecutionPayload{ParentHash: parent, BlockHash: blockHash, BlockNumber: blockNumber},
						},
					},
				},
			},
		},
		{
			name: "deneb blinded",
			proposal: &api.VersionedProposal{
				Version: spec.DataVersionDeneb,
				Blinded: true,
				DenebBlinded: &apiv1deneb.BlindedBeaconBlock{
					Body: &apiv1deneb.BlindedBeaconBlockBody{
						ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{ParentHash: parent, BlockHash: blockHash, BlockNumber: blockNumber},
					},
				},
			},
		},
		{
			name: "electra full",
			proposal: &api.VersionedProposal{
				Version: spec.DataVersionElectra,
				Electra: &apiv1electra.BlockContents{
					Block: &electra.BeaconBlock{
						Body: &electra.BeaconBlockBody{
							ExecutionPayload: &deneb.ExecutionPayload{ParentHash: parent, BlockHash: blockHash, BlockNumber: blockNumber},
						},
					},
				},
			},
		},
		{
			name: "electra blinded",
			proposal: &api.VersionedProposal{
				Version: spec.DataVersionElectra,
				Blinded: true,
				ElectraBlinded: &apiv1electra.BlindedBeaconBlock{
					Body: &apiv1electra.BlindedBeaconBlockBody{
						ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{ParentHash: parent, BlockHash: blockHash, BlockNumber: blockNumber},
					},
				},
			},
		},
		{
			name: "fulu full",
			proposal: &api.VersionedProposal{
				Version: spec.DataVersionFulu,
				Fulu: &apiv1fulu.BlockContents{
					Block: &electra.BeaconBlock{
						Body: &electra.BeaconBlockBody{
							ExecutionPayload: &deneb.ExecutionPayload{ParentHash: parent, BlockHash: blockHash, BlockNumber: blockNumber},
						},
					},
				},
			},
		},
		{
			name: "fulu blinded",
			proposal: &api.VersionedProposal{
				Version: spec.DataVersionFulu,
				Blinded: true,
				FuluBlinded: &apiv1electra.BlindedBeaconBlock{
					Body: &apiv1electra.BlindedBeaconBlockBody{
						ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{ParentHash: parent, BlockHash: blockHash, BlockNumber: blockNumber},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractExecutionInfo(tt.proposal)
			require.NoError(t, err)
			require.Equal(t, parent, got.ParentHash)
			require.Equal(t, blockHash, got.BlockHash)
			require.Equal(t, blockNumber, got.BlockNumber)
		})
	}
}
