package web3signer

import (
	"fmt"

	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
)

// ConvertBlockToBeaconBlockData converts various block types to Web3Signer BeaconBlockData format
func ConvertBlockToBeaconBlockData(obj ssz.HashRoot) (*BeaconBlockData, error) {
	switch v := obj.(type) {
	case *capella.BeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash beacon block (capella): %w", err)
		}

		return &BeaconBlockData{
			Version: DataVersion(spec.DataVersionCapella),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}, nil

	case *deneb.BeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash beacon block (deneb): %w", err)
		}

		return &BeaconBlockData{
			Version: DataVersion(spec.DataVersionDeneb),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}, nil

	case *electra.BeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash beacon block (electra): %w", err)
		}

		return &BeaconBlockData{
			Version: DataVersion(spec.DataVersionElectra),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}, nil

	case *apiv1capella.BlindedBeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash blinded beacon block (capella): %w", err)
		}

		return &BeaconBlockData{
			Version: DataVersion(spec.DataVersionCapella),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}, nil

	case *apiv1deneb.BlindedBeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash blinded beacon block (deneb): %w", err)
		}

		return &BeaconBlockData{
			Version: DataVersion(spec.DataVersionDeneb),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}, nil

	case *apiv1electra.BlindedBeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash blinded beacon block (electra): %w", err)
		}

		return &BeaconBlockData{
			Version: DataVersion(spec.DataVersionElectra),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}, nil

	default:
		return nil, fmt.Errorf("obj type is unknown: %T", obj)
	}
}
