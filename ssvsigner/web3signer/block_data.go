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
	"github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// ConvertBlockToBeaconBlockData converts a block to Web3Signer's BLOCK_V2 data: its header, whose root
// equals the block's. Fulu reuses Electra's block types, so the Electra cases cover both forks.
func ConvertBlockToBeaconBlockData(obj any, version spec.DataVersion) (*BeaconBlockData, error) {
	switch v := obj.(type) {
	case *capella.BeaconBlock:
		return blockHeaderData(version, v.Slot, v.ProposerIndex, v.ParentRoot, v.StateRoot, v.Body)
	case *deneb.BeaconBlock:
		return blockHeaderData(version, v.Slot, v.ProposerIndex, v.ParentRoot, v.StateRoot, v.Body)
	case *electra.BeaconBlock:
		return blockHeaderData(version, v.Slot, v.ProposerIndex, v.ParentRoot, v.StateRoot, v.Body)
	case *gloas.BeaconBlock:
		// Still a plain container, so the header root is the block's. ePBS has no blinded block: the payload
		// is in the envelope.
		return blockHeaderData(version, v.Slot, v.ProposerIndex, v.ParentRoot, v.StateRoot, v.Body)
	case *apiv1capella.BlindedBeaconBlock:
		return blockHeaderData(version, v.Slot, v.ProposerIndex, v.ParentRoot, v.StateRoot, v.Body)
	case *apiv1deneb.BlindedBeaconBlock:
		return blockHeaderData(version, v.Slot, v.ProposerIndex, v.ParentRoot, v.StateRoot, v.Body)
	case *apiv1electra.BlindedBeaconBlock:
		return blockHeaderData(version, v.Slot, v.ProposerIndex, v.ParentRoot, v.StateRoot, v.Body)
	default:
		return nil, fmt.Errorf("obj type is unknown: %T", obj)
	}
}

// blockHeaderData returns the BLOCK_V2 data of a block with the given fields.
func blockHeaderData(
	version spec.DataVersion,
	slot phase0.Slot,
	proposerIndex phase0.ValidatorIndex,
	parentRoot, stateRoot phase0.Root,
	body interface{ HashTreeRoot() ([32]byte, error) },
) (*BeaconBlockData, error) {
	bodyRoot, err := body.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("could not hash %T (%s): %w", body, version, err)
	}

	return &BeaconBlockData{
		Version: DataVersion(version),
		BlockHeader: &phase0.BeaconBlockHeader{
			Slot:          slot,
			ProposerIndex: proposerIndex,
			ParentRoot:    parentRoot,
			StateRoot:     stateRoot,
			BodyRoot:      bodyRoot,
		},
	}, nil
}
