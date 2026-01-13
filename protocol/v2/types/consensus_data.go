package types

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	AggCommCommIdxMismatchErrorCode     = 72
	AggCommUnusedCommIdxErrorCode       = 73
	AggCommDuplicatedCommIdxErrorCode   = 74
	AggCommSCCSubnetDuplicateErrorCode  = 76
	AggCommUnusedSubnetErrorCode        = 77
	UnknownVersionErrorCode             = 83
	AggCommAttestationDecodingErrorCode = 84
)

// AssignedAggregator represents a validator that has been assigned as an aggregator or sync committee contributor
type AssignedAggregator struct {
	ValidatorIndex phase0.ValidatorIndex
	SelectionProof phase0.BLSSignature `ssz-size:"96"`
	CommitteeIndex uint64
}

// AggregatorCommitteeConsensusData is the consensus data for the aggregator committee runner
// TODO: import it from spec after the Boole fork
type AggregatorCommitteeConsensusData struct {
	Version spec.DataVersion

	// Aggregator duties
	Aggregators []AssignedAggregator `ssz-max:"3000"` // For a maximum of 3k validators per committee
	// AggregatorsCommitteeIndexes is a list of committee indexes used by the above aggregators
	AggregatorsCommitteeIndexes []uint64 `ssz-max:"64"`
	// AggregatedAttestations is a list of aggregated attestations (SSZ bytes), one for each committee above
	AggregatedAttestations [][]byte `ssz-max:"64,131308"`

	// Sync Committee duties
	Contributors []AssignedAggregator `ssz-max:"2048"` // 512 * 4
	// SyncCommitteeContributions is a list of contributions, one for each subcommittee
	SyncCommitteeContributions []altair.SyncCommitteeContribution `ssz-max:"4"`
}

// Validate ensures the consensus data is internally consistent
func (a *AggregatorCommitteeConsensusData) Validate() error {
	// Ensure at least one validator
	if len(a.Aggregators) == 0 && len(a.Contributors) == 0 {
		return spectypes.NewError(spectypes.AggCommConsensusDataNoValidatorErrorCode, "no validators assigned to aggregator committee or sync committee")
	}

	// Aggregators validation

	// Ensure there is exactly one aggregated attestation per committee index
	if len(a.AggregatorsCommitteeIndexes) != len(a.AggregatedAttestations) {
		return spectypes.NewError(spectypes.AggCommAggCommIdxCntMismatchErrorCode, "committee indexes and attestations count mismatch")
	}

	// Validate equal set (AggregatorsCommitteeIndexes vs. Aggregators.CommitteeIndex)
	allowedAggCommittees := make(map[uint64]struct{}, len(a.AggregatorsCommitteeIndexes))
	for _, idx := range a.AggregatorsCommitteeIndexes {
		// Duplicates are not allowed
		if _, dup := allowedAggCommittees[idx]; dup {
			return spectypes.NewError(AggCommDuplicatedCommIdxErrorCode, "duplicate index in AggregatorsCommitteeIndexes")
		}
		allowedAggCommittees[idx] = struct{}{}
	}
	usedAggCommittees := make(map[uint64]struct{}, len(a.AggregatorsCommitteeIndexes))
	for _, agg := range a.Aggregators {
		// Check it exists in allowed
		if _, ok := allowedAggCommittees[agg.CommitteeIndex]; !ok {
			return spectypes.NewError(AggCommCommIdxMismatchErrorCode, "aggregator committee index not listed in AggregatorsCommitteeIndexes")
		}
		// Mark as used
		usedAggCommittees[agg.CommitteeIndex] = struct{}{}
	}
	// Ensure no committee index was left unused (no more than necessary)
	if len(usedAggCommittees) != len(allowedAggCommittees) {
		return spectypes.NewError(AggCommUnusedCommIdxErrorCode, "leftover aggregator committee index not usedAggCommittees by any aggregator")
	}

	// Ensure attestation objects are decoded correctly
	for _, attBytes := range a.AggregatedAttestations {
		if a.Version >= spec.DataVersionElectra {
			att := &electra.Attestation{}
			if err := att.UnmarshalSSZ(attBytes); err != nil {
				return spectypes.NewError(AggCommAttestationDecodingErrorCode, "failed to unmarshal attestation")
			}
		} else {
			att := &phase0.Attestation{}
			if err := att.UnmarshalSSZ(attBytes); err != nil {
				return spectypes.NewError(AggCommAttestationDecodingErrorCode, "failed to unmarshal attestation")
			}
		}
	}

	// Sync committee contributors validation

	// Validate equal set (Contributors.CommitteeIndex vs. SyncCommitteeContributions.SubcommitteeIndex)
	allowedSCSubnets := make(map[uint64]struct{}, len(a.SyncCommitteeContributions))
	for _, contrib := range a.SyncCommitteeContributions {
		// Duplicates are not allowed
		if _, dup := allowedSCSubnets[contrib.SubcommitteeIndex]; dup {
			return spectypes.NewError(AggCommSCCSubnetDuplicateErrorCode, "duplicate subcommittee index in SyncCommitteeContributions")
		}
		allowedSCSubnets[contrib.SubcommitteeIndex] = struct{}{}
	}
	usedSCSubnets := make(map[uint64]struct{}, len(a.SyncCommitteeContributions))
	for _, contributor := range a.Contributors {
		// Check it exists in allowed
		if _, ok := allowedSCSubnets[contributor.CommitteeIndex]; !ok {
			return spectypes.NewError(spectypes.AggCommSubnetNotInSCSubnetsErrorCode, "sync committee contributor subnet not listed in SyncCommitteeContributions")
		}
		// Mark as used
		usedSCSubnets[contributor.CommitteeIndex] = struct{}{}
	}
	// Ensure no subcommittee index was left unused (no more than necessary)
	if len(usedSCSubnets) != len(allowedSCSubnets) {
		return spectypes.NewError(AggCommUnusedSubnetErrorCode, "leftover sync committee contributor subnet not used in SyncCommitteeContributions")
	}

	return nil
}

// Encode encodes the consensus data to SSZ
func (a *AggregatorCommitteeConsensusData) Encode() ([]byte, error) {
	return a.MarshalSSZ()
}

// Decode decodes the consensus data from SSZ
func (a *AggregatorCommitteeConsensusData) Decode(data []byte) error {
	return a.UnmarshalSSZ(data)
}

// GetAggregateAndProofs returns all aggregate and proofs for the aggregator duties along with their hash roots
func (a *AggregatorCommitteeConsensusData) GetAggregateAndProofs() ([]*spec.VersionedAggregateAndProof, error) {
	proofs := make([]*spec.VersionedAggregateAndProof, 0, len(a.Aggregators))

	for _, aggregator := range a.Aggregators {
		// Decode attestation based on version
		var aggregateAndProof *spec.VersionedAggregateAndProof

		// Get index for validator in a.AggregatedAttestations
		foundIndex := -1
		for idx, committeeIndex := range a.AggregatorsCommitteeIndexes {
			if committeeIndex == aggregator.CommitteeIndex {
				foundIndex = idx
				break
			}
		}
		if foundIndex == -1 || foundIndex >= len(a.AggregatedAttestations) {
			return nil, spectypes.NewError(AggCommCommIdxMismatchErrorCode, "aggregator committee index not found for attestation")
		}

		switch a.Version {
		case spec.DataVersionPhase0, spec.DataVersionAltair, spec.DataVersionBellatrix, spec.DataVersionCapella, spec.DataVersionDeneb:
			agg := &phase0.AggregateAndProof{
				AggregatorIndex: aggregator.ValidatorIndex,
				SelectionProof:  aggregator.SelectionProof,
			}
			// Unmarshal the attestation
			att := &phase0.Attestation{}
			if err := att.UnmarshalSSZ(a.AggregatedAttestations[foundIndex]); err != nil {
				return nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("failed to unmarshal attestation: %w", err))
			}
			agg.Aggregate = att

			aggregateAndProof = &spec.VersionedAggregateAndProof{
				Version: a.Version,
			}
			// Set the appropriate version field and store hash root
			switch a.Version {
			case spec.DataVersionPhase0:
				aggregateAndProof.Phase0 = agg
			case spec.DataVersionAltair:
				aggregateAndProof.Altair = agg
			case spec.DataVersionBellatrix:
				aggregateAndProof.Bellatrix = agg
			case spec.DataVersionCapella:
				aggregateAndProof.Capella = agg
			case spec.DataVersionDeneb:
				aggregateAndProof.Deneb = agg
			default:
				panic("unhandled default case")
			}

		case spec.DataVersionElectra, spec.DataVersionFulu:
			agg := &electra.AggregateAndProof{
				AggregatorIndex: aggregator.ValidatorIndex,
				SelectionProof:  aggregator.SelectionProof,
			}
			// Unmarshal the attestation
			att := &electra.Attestation{}
			if err := att.UnmarshalSSZ(a.AggregatedAttestations[foundIndex]); err != nil {
				return nil, spectypes.WrapError(spectypes.UnmarshalSSZErrorCode, fmt.Errorf("failed to unmarshal electra attestation: %w", err))
			}
			agg.Aggregate = att

			aggregateAndProof = &spec.VersionedAggregateAndProof{
				Version: a.Version,
			}

			switch a.Version {
			case spec.DataVersionElectra:
				aggregateAndProof.Electra = agg
			case spec.DataVersionFulu:
				aggregateAndProof.Fulu = agg
			default:
				panic("unhandled default case")
			}

		default:
			return nil, spectypes.WrapError(spectypes.UnknownBlockVersionErrorCode, fmt.Errorf("unsupported version %s", a.Version.String()))
		}

		proofs = append(proofs, aggregateAndProof)
	}

	return proofs, nil
}

// GetSyncCommitteeContributions returns the sync committee contributions
func (a *AggregatorCommitteeConsensusData) GetSyncCommitteeContributions() (spectypes.Contributions, error) {
	contributions := make(spectypes.Contributions, 0, len(a.Contributors))

	for _, contributor := range a.Contributors {
		// Find associated object in a.SyncCommitteeContributions
		foundIndex := -1
		for idx, contrib := range a.SyncCommitteeContributions {
			if contrib.SubcommitteeIndex == contributor.CommitteeIndex {
				foundIndex = idx
				break
			}
		}
		if foundIndex == -1 {
			return nil, spectypes.NewError(spectypes.AggCommSubnetNotInSCSubnetsErrorCode, "sync committee contributor subnet not found in SyncCommitteeContributions")
		}

		var sigBytes [96]byte
		copy(sigBytes[:], contributor.SelectionProof[:])
		contributions = append(contributions, &spectypes.Contribution{
			SelectionProofSig: sigBytes,
			Contribution:      a.SyncCommitteeContributions[foundIndex],
		})
	}

	return contributions, nil
}
