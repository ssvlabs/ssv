package types

// ssv-spec is Gloas-only: it assumes the Gloas fork has activated, and leaves the transition across it to
// implementations. The node still runs pre-Gloas slots, so this file keeps the pre-Gloas parts of the spec
// it previously pinned. Each helper takes every fork: it handles pre-Gloas data itself and delegates the
// rest to the spec.

import (
	"fmt"

	"github.com/OffchainLabs/go-bitfield"
	"github.com/attestantio/go-eth2-client/api"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	apiv1fulu "github.com/attestantio/go-eth2-client/api/v1/fulu"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// PreGloasBeaconVote is the committee runner's consensus value before Gloas: the head block root and the
// FFG checkpoints, 112 bytes of SSZ. From Gloas, the spec's BeaconVote, which also carries the attestation
// data index, takes its place.
type PreGloasBeaconVote struct {
	BlockRoot phase0.Root `ssz-size:"32"`
	Source    *phase0.Checkpoint
	Target    *phase0.Checkpoint
}

// Encode encodes the vote as SSZ.
func (b *PreGloasBeaconVote) Encode() ([]byte, error) {
	return b.MarshalSSZ()
}

// Decode decodes the vote from SSZ.
func (b *PreGloasBeaconVote) Decode(data []byte) error {
	return b.UnmarshalSSZ(data)
}

// Validate checks that both checkpoints are set, and that the source precedes the target.
func (b *PreGloasBeaconVote) Validate() error {
	if b == nil {
		return spectypes.NewError(spectypes.BeaconVoteNilCheckpointErrorCode, "nil beacon vote")
	}
	if b.Source == nil || b.Target == nil {
		return spectypes.NewError(spectypes.BeaconVoteNilCheckpointErrorCode, "nil source or target checkpoint")
	}
	if b.Source.Epoch >= b.Target.Epoch {
		return spectypes.NewError(spectypes.AttestationSourceNotLessThanTargetErrorCode, "attestation data source >= target")
	}

	return nil
}

// PreGloasBlockData decodes a pre-Gloas proposal, blinded or not, and returns it with the block to sign.
func PreGloasBlockData(cd *spectypes.ProposerConsensusData) (*api.VersionedProposal, spectypes.HashRoot, error) {
	switch cd.Version {
	case spec.DataVersionCapella:
		blindedBlock := &apiv1capella.BlindedBeaconBlock{}
		blindedErr := blindedBlock.UnmarshalSSZ(cd.DataSSZ)
		if blindedErr == nil {
			return &api.VersionedProposal{Version: cd.Version, Blinded: true, CapellaBlinded: blindedBlock}, blindedBlock, nil
		}

		regularBlock := &capella.BeaconBlock{}
		regularErr := regularBlock.UnmarshalSSZ(cd.DataSSZ)
		if regularErr == nil {
			return &api.VersionedProposal{Capella: regularBlock, Version: cd.Version}, regularBlock, nil
		}

		return nil, nil, blockDataError(blindedErr, regularErr)
	case spec.DataVersionDeneb:
		blindedBlock := &apiv1deneb.BlindedBeaconBlock{}
		blindedErr := blindedBlock.UnmarshalSSZ(cd.DataSSZ)
		if blindedErr == nil {
			return &api.VersionedProposal{Version: cd.Version, Blinded: true, DenebBlinded: blindedBlock}, blindedBlock, nil
		}

		regularContents := &apiv1deneb.BlockContents{}
		regularErr := regularContents.UnmarshalSSZ(cd.DataSSZ)
		if regularErr == nil {
			return &api.VersionedProposal{Deneb: regularContents, Version: cd.Version}, regularContents.Block, nil
		}

		return nil, nil, blockDataError(blindedErr, regularErr)
	case spec.DataVersionElectra:
		blindedBlock := &apiv1electra.BlindedBeaconBlock{}
		blindedErr := blindedBlock.UnmarshalSSZ(cd.DataSSZ)
		if blindedErr == nil {
			return &api.VersionedProposal{Version: cd.Version, Blinded: true, ElectraBlinded: blindedBlock}, blindedBlock, nil
		}

		regularContents := &apiv1electra.BlockContents{}
		regularErr := regularContents.UnmarshalSSZ(cd.DataSSZ)
		if regularErr == nil {
			return &api.VersionedProposal{Electra: regularContents, Version: cd.Version}, regularContents.Block, nil
		}

		return nil, nil, blockDataError(blindedErr, regularErr)
	case spec.DataVersionFulu:
		blindedBlock := &apiv1electra.BlindedBeaconBlock{}
		blindedErr := blindedBlock.UnmarshalSSZ(cd.DataSSZ)
		if blindedErr == nil {
			return &api.VersionedProposal{Version: cd.Version, Blinded: true, FuluBlinded: blindedBlock}, blindedBlock, nil
		}

		regularContents := &apiv1fulu.BlockContents{}
		regularErr := regularContents.UnmarshalSSZ(cd.DataSSZ)
		if regularErr == nil {
			return &api.VersionedProposal{Fulu: regularContents, Version: cd.Version}, regularContents.Block, nil
		}

		return nil, nil, blockDataError(blindedErr, regularErr)
	default:
		return nil, nil, spectypes.WrapError(spectypes.UnknownBlockVersionErrorCode, fmt.Errorf("unknown block version %d", cd.Version))
	}
}

func blockDataError(blindedErr, regularErr error) error {
	return spectypes.WrapError(spectypes.UnmarshalSSZErrorCode,
		fmt.Errorf("could not unmarshal ssz (blinded err: %w, regular err: %w)", blindedErr, regularErr))
}

// ValidateAggregatorCommitteeConsensusData is (*spectypes.AggregatorCommitteeConsensusData).Validate for
// every fork.
func ValidateAggregatorCommitteeConsensusData(a *spectypes.AggregatorCommitteeConsensusData) error {
	if a.Version >= spec.DataVersionGloas {
		return a.Validate()
	}

	if len(a.Aggregators) == 0 && len(a.Contributors) == 0 {
		return spectypes.NewError(spectypes.AggCommConsensusDataNoValidatorErrorCode,
			"no validators assigned to aggregator committee or sync committee")
	}

	// Exactly one aggregated attestation per committee index, each index used by an aggregator.
	if len(a.AggregatorsCommitteeIndexes) != len(a.AggregatedAttestations) {
		return spectypes.NewError(spectypes.AggCommAggCommIdxCntMismatchErrorCode, "committee indexes and attestations count mismatch")
	}
	allowedAggCommittees := make(map[uint64]struct{}, len(a.AggregatorsCommitteeIndexes))
	for _, idx := range a.AggregatorsCommitteeIndexes {
		if _, dup := allowedAggCommittees[idx]; dup {
			return spectypes.NewError(spectypes.AggCommDuplicatedCommIdxErrorCode, "duplicate index in AggregatorsCommitteeIndexes")
		}
		allowedAggCommittees[idx] = struct{}{}
	}
	usedAggCommittees := make(map[uint64]struct{}, len(a.AggregatorsCommitteeIndexes))
	for _, agg := range a.Aggregators {
		if _, ok := allowedAggCommittees[agg.CommitteeIndex]; !ok {
			return spectypes.NewError(spectypes.AggCommCommIdxMismatchErrorCode,
				"aggregator committee index not listed in AggregatorsCommitteeIndexes")
		}
		usedAggCommittees[agg.CommitteeIndex] = struct{}{}
	}
	if len(usedAggCommittees) != len(allowedAggCommittees) {
		return spectypes.NewError(spectypes.AggCommUnusedCommIdxErrorCode,
			"leftover aggregator committee index not usedAggCommittees by any aggregator")
	}

	for _, attBytes := range a.AggregatedAttestations {
		if err := preGloasAttestation(a.Version).UnmarshalSSZ(attBytes); err != nil {
			return spectypes.NewError(spectypes.AggCommAttestationDecodingErrorCode, "failed to unmarshal attestation")
		}
	}

	// Exactly one contribution per subcommittee index, each index used by a contributor.
	allowedSCSubnets := make(map[uint64]struct{}, len(a.SyncCommitteeContributions))
	for _, contrib := range a.SyncCommitteeContributions {
		if _, dup := allowedSCSubnets[contrib.SubcommitteeIndex]; dup {
			return spectypes.NewError(spectypes.AggCommSCCSubnetDuplicateErrorCode,
				"duplicate subcommittee index in SyncCommitteeContributions")
		}
		allowedSCSubnets[contrib.SubcommitteeIndex] = struct{}{}
	}
	usedSCSubnets := make(map[uint64]struct{}, len(a.SyncCommitteeContributions))
	for _, contributor := range a.Contributors {
		if _, ok := allowedSCSubnets[contributor.CommitteeIndex]; !ok {
			return spectypes.NewError(spectypes.AggCommSubnetNotInSCSubnetsErrorCode,
				"sync committee contributor subnet not listed in SyncCommitteeContributions")
		}
		usedSCSubnets[contributor.CommitteeIndex] = struct{}{}
	}
	if len(usedSCSubnets) != len(allowedSCSubnets) {
		return spectypes.NewError(spectypes.AggCommUnusedSubnetErrorCode,
			"leftover sync committee contributor subnet not used in SyncCommitteeContributions")
	}

	return nil
}

// preGloasAttestation returns an empty attestation of the shape a pre-Gloas fork encodes.
func preGloasAttestation(version spec.DataVersion) interface{ UnmarshalSSZ([]byte) error } {
	if version >= spec.DataVersionElectra {
		return &electra.Attestation{}
	}

	return &phase0.Attestation{}
}

// GetAggregateAndProofs is (*spectypes.AggregatorCommitteeConsensusData).GetAggregateAndProofs for every
// fork.
func GetAggregateAndProofs(a *spectypes.AggregatorCommitteeConsensusData) ([]*spec.VersionedAggregateAndProof, error) {
	if a.Version >= spec.DataVersionGloas {
		return a.GetAggregateAndProofs()
	}

	proofs := make([]*spec.VersionedAggregateAndProof, 0, len(a.Aggregators))
	for _, aggregator := range a.Aggregators {
		foundIndex := -1
		for idx, committeeIndex := range a.AggregatorsCommitteeIndexes {
			if committeeIndex == aggregator.CommitteeIndex {
				foundIndex = idx
				break
			}
		}
		if foundIndex == -1 || foundIndex >= len(a.AggregatedAttestations) {
			return nil, spectypes.NewError(spectypes.AggCommCommIdxMismatchErrorCode, "aggregator committee index not found for attestation")
		}
		attBytes := a.AggregatedAttestations[foundIndex]

		aggregateAndProof := &spec.VersionedAggregateAndProof{Version: a.Version}
		switch a.Version {
		case spec.DataVersionPhase0, spec.DataVersionAltair, spec.DataVersionBellatrix, spec.DataVersionCapella, spec.DataVersionDeneb:
			att := &phase0.Attestation{}
			if err := att.UnmarshalSSZ(attBytes); err != nil {
				return nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("failed to unmarshal attestation: %w", err))
			}
			agg := &phase0.AggregateAndProof{
				AggregatorIndex: aggregator.ValidatorIndex,
				Aggregate:       att,
				SelectionProof:  aggregator.SelectionProof,
			}
			switch a.Version {
			case spec.DataVersionPhase0:
				aggregateAndProof.Phase0 = agg
			case spec.DataVersionAltair:
				aggregateAndProof.Altair = agg
			case spec.DataVersionBellatrix:
				aggregateAndProof.Bellatrix = agg
			case spec.DataVersionCapella:
				aggregateAndProof.Capella = agg
			default:
				aggregateAndProof.Deneb = agg
			}
		case spec.DataVersionElectra, spec.DataVersionFulu:
			att := &electra.Attestation{}
			if err := att.UnmarshalSSZ(attBytes); err != nil {
				return nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("failed to unmarshal electra attestation: %w", err))
			}
			agg := &electra.AggregateAndProof{
				AggregatorIndex: aggregator.ValidatorIndex,
				Aggregate:       att,
				SelectionProof:  aggregator.SelectionProof,
			}
			if a.Version == spec.DataVersionElectra {
				aggregateAndProof.Electra = agg
			} else {
				aggregateAndProof.Fulu = agg
			}
		default:
			return nil, spectypes.WrapError(spectypes.UnknownBlockVersionErrorCode, fmt.Errorf("unsupported version %s", a.Version.String()))
		}

		proofs = append(proofs, aggregateAndProof)
	}

	return proofs, nil
}

// GetAggregateAndProofHashRoot is spectypes.GetAggregateAndProofHashRoot for every fork.
func GetAggregateAndProofHashRoot(aggProof *spec.VersionedAggregateAndProof) (spectypes.HashRoot, error) {
	switch aggProof.Version {
	case spec.DataVersionPhase0:
		return aggProof.Phase0, nil
	case spec.DataVersionAltair:
		return aggProof.Altair, nil
	case spec.DataVersionBellatrix:
		return aggProof.Bellatrix, nil
	case spec.DataVersionCapella:
		return aggProof.Capella, nil
	case spec.DataVersionDeneb:
		return aggProof.Deneb, nil
	case spec.DataVersionElectra:
		return aggProof.Electra, nil
	case spec.DataVersionFulu:
		return aggProof.Fulu, nil
	default:
		return spectypes.GetAggregateAndProofHashRoot(aggProof)
	}
}

// ConstructVersionedAttestationWithoutSignature is the spec's ssv.ConstructVersionedAttestationWithoutSignature
// for every fork.
func ConstructVersionedAttestationWithoutSignature(
	attestationData *phase0.AttestationData,
	dataVersion spec.DataVersion,
	validatorDuty *spectypes.ValidatorDuty,
) (*spec.VersionedAttestation, error) {
	ret := &spec.VersionedAttestation{
		Version:        dataVersion,
		ValidatorIndex: &validatorDuty.ValidatorIndex,
	}

	switch dataVersion {
	case spec.DataVersionPhase0:
		ret.Phase0 = phase0AttestationWithoutSignature(attestationData, validatorDuty)
	case spec.DataVersionAltair:
		ret.Altair = phase0AttestationWithoutSignature(attestationData, validatorDuty)
	case spec.DataVersionBellatrix:
		ret.Bellatrix = phase0AttestationWithoutSignature(attestationData, validatorDuty)
	case spec.DataVersionCapella:
		ret.Capella = phase0AttestationWithoutSignature(attestationData, validatorDuty)
	case spec.DataVersionDeneb:
		ret.Deneb = phase0AttestationWithoutSignature(attestationData, validatorDuty)
	case spec.DataVersionElectra:
		ret.Electra = electraAttestationWithoutSignature(attestationData, validatorDuty)
	case spec.DataVersionFulu:
		ret.Fulu = electraAttestationWithoutSignature(attestationData, validatorDuty)
	case spec.DataVersionGloas:
		return specssv.ConstructVersionedAttestationWithoutSignature(attestationData, validatorDuty), nil
	default:
		return nil, fmt.Errorf("unknown version")
	}

	return ret, nil
}

func phase0AttestationWithoutSignature(attestationData *phase0.AttestationData, validatorDuty *spectypes.ValidatorDuty) *phase0.Attestation {
	aggregationBitfield := bitfield.NewBitlist(validatorDuty.CommitteeLength)
	aggregationBitfield.SetBitAt(validatorDuty.ValidatorCommitteeIndex, true)

	return &phase0.Attestation{
		Data:            attestationData,
		AggregationBits: aggregationBitfield,
	}
}

func electraAttestationWithoutSignature(attestationData *phase0.AttestationData, validatorDuty *spectypes.ValidatorDuty) *electra.Attestation {
	aggregationBitfield := bitfield.NewBitlist(validatorDuty.CommitteeLength)
	aggregationBitfield.SetBitAt(validatorDuty.ValidatorCommitteeIndex, true)

	committeeBits := bitfield.NewBitvector64()
	committeeBits.SetBitAt(uint64(validatorDuty.CommitteeIndex), true)

	return &electra.Attestation{
		Data:            attestationData,
		AggregationBits: aggregationBitfield,
		CommitteeBits:   committeeBits,
	}
}

// VersionedAttestationWithSignature is the spec's ssv.VersionedAttestationWithSignature for every fork.
func VersionedAttestationWithSignature(att *spec.VersionedAttestation, sig phase0.BLSSignature) (*spec.VersionedAttestation, error) {
	switch att.Version {
	case spec.DataVersionPhase0:
		if att.Phase0 == nil {
			return att, fmt.Errorf("no Phase0 attestation")
		}
		att.Phase0.Signature = sig
	case spec.DataVersionAltair:
		if att.Altair == nil {
			return att, fmt.Errorf("no Altair attestation")
		}
		att.Altair.Signature = sig
	case spec.DataVersionBellatrix:
		if att.Bellatrix == nil {
			return att, fmt.Errorf("no Bellatrix attestation")
		}
		att.Bellatrix.Signature = sig
	case spec.DataVersionCapella:
		if att.Capella == nil {
			return att, fmt.Errorf("no Capella attestation")
		}
		att.Capella.Signature = sig
	case spec.DataVersionDeneb:
		if att.Deneb == nil {
			return att, fmt.Errorf("no Deneb attestation")
		}
		att.Deneb.Signature = sig
	case spec.DataVersionElectra:
		if att.Electra == nil {
			return att, fmt.Errorf("no Electra attestation")
		}
		att.Electra.Signature = sig
	case spec.DataVersionFulu:
		if att.Fulu == nil {
			return att, fmt.Errorf("no Fulu attestation")
		}
		att.Fulu.Signature = sig
	default:
		return specssv.VersionedAttestationWithSignature(att, sig)
	}

	return att, nil
}
