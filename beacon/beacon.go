package beacon

import (
	"context"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	ikeymanager "github.com/prysmaticlabs/prysm/validator/keymanager"
)

// Role represents the validator role for a specific duty
type Role int

func (r Role) String() string {
	switch r {
	case RoleUnknown:
		return "UNKNOWN"
	case RoleAttester:
		return "ATTESTER"
	case RoleAggregator:
		return "AGGREGATOR"
	case RoleProposer:
		return "PROPOSER"
	default:
		return "UNDEFINED"
	}
}

// List of roles
const (
	RoleUnknown = iota
	RoleAttester
	RoleAggregator
	RoleProposer
)

// Beacon represents the behavior of the beacon node connector
type Beacon interface {
	// StreamDuties returns channel with duties stream
	StreamDuties(ctx context.Context, pubKey []byte) (<-chan *ethpb.DutiesResponse_Duty, error)

	// GetAttestationData returns attestation data by the given slot and committee index
	GetAttestationData(ctx context.Context, slot, committeeIndex uint64) (*ethpb.AttestationData, error)

	// SubmitAttestation submits attestation fo the given slot using the given public key
	SubmitAttestation(ctx context.Context, data *ethpb.AttestationData, duty *ethpb.DutiesResponse_Duty, keyManager ikeymanager.IKeymanager) error

	// GetAggregationData returns aggregation data for the given slot and committee index
	GetAggregationData(ctx context.Context, slot, committeeIndex uint64) (*ethpb.SignedAggregateAttestationAndProof, error)

	// GetProposalData returns proposal block for the given slot
	GetProposalData(ctx context.Context, slot uint64) (*ethpb.BeaconBlock, error)

	// RolesAt slot returns the validator roles at the given slot. Returns nil if the
	// validator is known to not have a roles at the at slot. Returns UNKNOWN if the
	// validator assignments are unknown. Otherwise returns a valid validatorRole map.
	RolesAt(ctx context.Context, slot uint64, duty *ethpb.DutiesResponse_Duty) ([]Role, error)
}
