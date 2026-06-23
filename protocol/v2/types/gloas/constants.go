// Package gloas holds the node-side protocol/wire types and constants introduced by
// ePBS (EIP-7732 / Gloas), per SIP ssvlabs/SIPs#94. They are defined node-side as
// values of the existing spectypes base types, slotting above the consolidated Boole
// roles (max RoleAggregatorCommittee = 6).
//
// The wire values here are protocol-canonical and MUST match the Anchor (Rust) client;
// do not change them without a coordinated cross-client + SIP update.
package gloas

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Runner roles for the three new ePBS duties. The deprecated RunnerRole 1/3
// (RoleAggregator / RoleSyncCommitteeContribution) stay reserved for pre-consolidation
// back-compat decoding — see protocol/v2/types/runner_role.go.
const (
	RolePTCAttester         = spectypes.RunnerRole(7) // §3 payload-attestation (PTC) attester
	RoleProposerPreferences = spectypes.RunnerRole(8) // §5 proposer preferences
	RoleEnvelopeBuilder     = spectypes.RunnerRole(9) // §6 execution-payload envelope
)

// Beacon (duty) roles mirroring the runner roles above. Existing BeaconRole values
// run 0..6 (BNRoleVoluntaryExit = 6), so 7/8/9 are the next free slots.
const (
	BNRolePTCAttester         = spectypes.BeaconRole(7)
	BNRoleProposerPreferences = spectypes.BeaconRole(8)
	BNRoleEnvelopeBuilder     = spectypes.BeaconRole(9)
)

// Partial-signature message types. Existing values run up to
// AggregatorCommitteePartialSig = 6. The §6 envelope duty adds no type of its own:
// its post-consensus reuses PostConsensusPartialSig, discriminated by runner role.
const (
	// PTCAttesterPartialSig is the partial signature over PayloadAttestationData (§3).
	PTCAttesterPartialSig = spectypes.PartialSigMsgType(7)
	// ProposerPreferencesPartialSig is the partial signature over ProposerPreferences (§5).
	ProposerPreferencesPartialSig = spectypes.PartialSigMsgType(8)
)

// Beacon signing domains introduced by Gloas — consensus-spec domains (4 bytes,
// domain number in byte[0], matching the spectypes.Domain* style).
var (
	// DomainBeaconBuilder signs the (blinded) ExecutionPayloadEnvelope (§6).
	DomainBeaconBuilder = [4]byte{0x0b, 0x00, 0x00, 0x00}
	// DomainPTCAttester signs PayloadAttestationData (§3).
	DomainPTCAttester = [4]byte{0x0c, 0x00, 0x00, 0x00}
	// DomainProposerPreferences signs ProposerPreferences (§5).
	DomainProposerPreferences = [4]byte{0x0d, 0x00, 0x00, 0x00}
)

// RunnerRoleForBeaconRole maps a Gloas (ePBS) beacon duty role to its runner role,
// reporting ok=false for non-Gloas roles. ssv-spec's ValidatorDuty.RunnerRole() predates
// these roles, so callers apply this mapping node-side before delegating to it.
func RunnerRoleForBeaconRole(role spectypes.BeaconRole) (spectypes.RunnerRole, bool) {
	switch role {
	case BNRolePTCAttester:
		return RolePTCAttester, true
	case BNRoleProposerPreferences:
		return RoleProposerPreferences, true
	case BNRoleEnvelopeBuilder:
		return RoleEnvelopeBuilder, true
	default:
		return spectypes.RoleUnknown, false
	}
}
