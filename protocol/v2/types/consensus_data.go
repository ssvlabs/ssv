package types

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func GetAggregateAndProof(cd *spectypes.ProposerConsensusData) (*spec.VersionedAggregateAndProof, ssz.HashRoot, error) {
	switch cd.Version {
	case spec.DataVersionPhase0:
		ret := &phase0.AggregateAndProof{}
		if err := ret.UnmarshalSSZ(cd.DataSSZ); err != nil {
			return nil, nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("could not unmarshal ssz: %w", err))
		}

		return &spec.VersionedAggregateAndProof{Version: cd.Version, Phase0: ret}, ret, nil
	case spec.DataVersionAltair:
		ret := &phase0.AggregateAndProof{}
		if err := ret.UnmarshalSSZ(cd.DataSSZ); err != nil {
			return nil, nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("could not unmarshal ssz: %w", err))
		}

		return &spec.VersionedAggregateAndProof{Version: cd.Version, Altair: ret}, ret, nil
	case spec.DataVersionBellatrix:
		ret := &phase0.AggregateAndProof{}
		if err := ret.UnmarshalSSZ(cd.DataSSZ); err != nil {
			return nil, nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("could not unmarshal ssz: %w", err))
		}

		return &spec.VersionedAggregateAndProof{Version: cd.Version, Bellatrix: ret}, ret, nil
	case spec.DataVersionCapella:
		ret := &phase0.AggregateAndProof{}
		if err := ret.UnmarshalSSZ(cd.DataSSZ); err != nil {
			return nil, nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("could not unmarshal ssz: %w", err))
		}

		return &spec.VersionedAggregateAndProof{Version: cd.Version, Capella: ret}, ret, nil
	case spec.DataVersionDeneb:
		ret := &phase0.AggregateAndProof{}
		if err := ret.UnmarshalSSZ(cd.DataSSZ); err != nil {
			return nil, nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("could not unmarshal ssz: %w", err))
		}

		return &spec.VersionedAggregateAndProof{Version: cd.Version, Deneb: ret}, ret, nil
	case spec.DataVersionElectra:
		ret := &electra.AggregateAndProof{}
		if err := ret.UnmarshalSSZ(cd.DataSSZ); err != nil {
			return nil, nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("could not unmarshal ssz: %w", err))
		}

		return &spec.VersionedAggregateAndProof{Version: cd.Version, Electra: ret}, ret, nil
	case spec.DataVersionFulu:
		ret := &electra.AggregateAndProof{}
		if err := ret.UnmarshalSSZ(cd.DataSSZ); err != nil {
			return nil, nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("could not unmarshal ssz: %w", err))
		}

		return &spec.VersionedAggregateAndProof{Version: cd.Version, Fulu: ret}, ret, nil
	default:
		return nil, nil, fmt.Errorf("unknown aggregate and proof version %d", cd.Version)
	}
}

func GetSyncCommitteeContributions(cd *spectypes.ProposerConsensusData) (spectypes.Contributions, error) {
	ret := spectypes.Contributions{}
	if err := ret.UnmarshalSSZ(cd.DataSSZ); err != nil {
		return nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("could not unmarshal ssz: %w", err))
	}
	return ret, nil
}
