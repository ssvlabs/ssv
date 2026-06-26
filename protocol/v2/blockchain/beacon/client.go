package beacon

import (
	"context"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

//go:generate go tool -modfile=../../../../tool.mod mockgen -package=beacon -destination=./mock_client.go -source=./client.go

// AttesterCalls interface has all attester duty specific calls
type AttesterCalls interface {
	// GetAttestationData returns attestation data by the given slot and committee index
	GetAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, spec.DataVersion, error)
	// SubmitAttestations submits the attestation to the node
	SubmitAttestations(ctx context.Context, attestations []*spec.VersionedAttestation) error
}

// ProposerCalls interface has all block proposer duty specific calls
type ProposerCalls interface {
	// GetBeaconBlock returns beacon block by the given slot, graffiti, and randao.
	// Returns:
	//   - *api.VersionedProposal: The full versioned proposal containing all block variants
	//   - ssz.Marshaler: The specific versioned block for the current fork (e.g., beaconBlock.Capella, beaconBlock.Deneb)
	//   - error: Any error encountered during block retrieval
	GetBeaconBlock(ctx context.Context, slot phase0.Slot, graffiti, randao []byte) (*api.VersionedProposal, ssz.Marshaler, error)
	// SubmitBeaconBlock submit the block to the node
	SubmitBeaconBlock(ctx context.Context, block *api.VersionedProposal, sig phase0.BLSSignature) error
}

// AggregatorCalls interface has all attestation aggregator duty specific calls
type AggregatorCalls interface {
	// IsAggregator returns true if the validator is selected as an aggregator
	IsAggregator(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, slotSig []byte) bool
	// GetAggregateAttestation returns the aggregate attestation for the given slot and committee
	GetAggregateAttestation(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (ssz.Marshaler, spec.DataVersion, error)
	// SubmitAggregateSelectionProof returns an AggregateAndProof object
	// Deprecated: Use IsAggregator and GetAggregateAttestation instead. Kept for backward compatibility.
	SubmitAggregateSelectionProof(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, index phase0.ValidatorIndex, slotSig []byte) (ssz.Marshaler, spec.DataVersion, error)
	// SubmitSignedAggregateSelectionProof broadcasts a signed aggregator msg
	SubmitSignedAggregateSelectionProof(ctx context.Context, msg *spec.VersionedSignedAggregateAndProof) error
}

// SyncCommitteeCalls interface has all sync committee duty specific calls
type SyncCommitteeCalls interface {
	// SubmitSyncMessages submits signed sync committee messages
	SubmitSyncMessages(ctx context.Context, msgs []*altair.SyncCommitteeMessage) error
}

// SyncCommitteeContributionCalls interface has all sync committee contribution duty specific calls
type SyncCommitteeContributionCalls interface {
	// IsSyncCommitteeAggregator returns true if aggregator
	IsSyncCommitteeAggregator(proof []byte) bool
	// SyncCommitteeSubnetID returns sync committee subnet ID from subcommittee index
	SyncCommitteeSubnetID(index phase0.CommitteeIndex) uint64
	// GetSyncCommitteeContribution returns a types.Contributions object
	GetSyncCommitteeContribution(ctx context.Context, slot phase0.Slot, selectionProofs []phase0.BLSSignature, subnetIDs []uint64) (ssz.Marshaler, spec.DataVersion, error)
	// SubmitSignedContributionAndProof broadcasts to the network
	SubmitSignedContributionAndProof(ctx context.Context, contribution *altair.SignedContributionAndProof) error
}

// ValidatorRegistrationCalls interface has all validator registration duty specific calls
type ValidatorRegistrationCalls interface {
	// SubmitValidatorRegistrations submits validator registrations, chunking it if necessary.
	SubmitValidatorRegistrations(ctx context.Context, registrations []*api.VersionedSignedValidatorRegistration) error
}

// VoluntaryExitCalls interface has all validator voluntary exit duty specific calls
type VoluntaryExitCalls interface {
	// SubmitVoluntaryExit submits a validator voluntary exit
	SubmitVoluntaryExit(ctx context.Context, voluntaryExit *phase0.SignedVoluntaryExit) error
}

type DomainCalls interface {
	DomainData(ctx context.Context, epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error)
}

// beaconDuties interface serves all duty related calls
type beaconDuties interface {
	AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error)
	ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error)
	SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error)
	SubscribeToHeadEvents(ctx context.Context, subscriberIdentifier string, ch chan<- *eth2apiv1.HeadEvent) error
}

// beaconSubscriber interface serves all committee subscribe to subnet (p2p topic)
type beaconSubscriber interface {
	// SubmitBeaconCommitteeSubscriptions subscribe committee to subnet
	SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error
	// SubmitSyncCommitteeSubscriptions subscribe to sync committee subnet
	SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error
}

type beaconValidator interface {
	// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
	GetValidatorData(ctx context.Context, validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error)
}

type proposalPreparations interface {
	// SubmitProposalPreparations submits proposal preparations
	SubmitProposalPreparations(ctx context.Context, preparations []*eth2apiv1.ProposalPreparation) error
	// SetProposalPreparationsProvider sets a callback to retrieve current proposal preparations
	// This is used to re-submit preparations when beacon nodes reconnect
	SetProposalPreparationsProvider(provider func() ([]*eth2apiv1.ProposalPreparation, error))
}

// TODO need to handle differently (by spec)
type signer interface {
	ComputeSigningRoot(object any, domain phase0.Domain) ([32]byte, error)
}

// TODO: remove temp spec intefaces once spec is settled

// BeaconNode interface for all beacon duty calls
type BeaconNode interface {
	AttesterCalls
	ProposerCalls
	AggregatorCalls
	SyncCommitteeCalls
	SyncCommitteeContributionCalls
	ValidatorRegistrationCalls
	VoluntaryExitCalls
	PTCCalls
	ProposerPreferencesCalls
	GloasProposerCalls
	GloasEnvelopeCalls
	DomainCalls

	beaconDuties
	beaconSubscriber
	beaconValidator
	signer // TODO need to handle differently
	proposalPreparations
}

// PTCCalls is the beacon-node surface for Gloas (ePBS) Payload Timeliness Committee duties:
// fetching assignments, producing the data to attest to, and submitting signed messages.
type PTCCalls interface {
	// PayloadAttestationDuties returns the PTC duties for the given validators at the epoch.
	PayloadAttestationDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*gloas.PTCDuty, error)
	// PayloadAttestationData returns the PayloadAttestationData to attest to for the slot.
	PayloadAttestationData(ctx context.Context, slot phase0.Slot) (*gloas.PayloadAttestationData, error)
	// SubmitPayloadAttestationMessages submits signed PTC messages to the beacon node's pool.
	SubmitPayloadAttestationMessages(ctx context.Context, messages []*gloas.PayloadAttestationMessage) error
}

// ProposerPreferencesCalls is the beacon-node surface for Gloas (ePBS) proposer preferences.
// Publication has no beacon-API endpoint upstream yet (SIP #94 §5), so SubmitProposerPreferences
// returns the gloas.ErrProposerPreferencesPublishUnavailable sentinel for now (see
// beacon/goclient/proposer_preferences.go).
type ProposerPreferencesCalls interface {
	// ProposerDutiesDependentRoot returns the proposer-duties dependent root for the epoch — the
	// seed the proposer-lookahead is pinned to. go-eth2-client drops it, so it's fetched via raw HTTP.
	ProposerDutiesDependentRoot(ctx context.Context, epoch phase0.Epoch) (phase0.Root, error)
	// SubmitProposerPreferences broadcasts signed proposer preferences for upcoming proposal slots.
	SubmitProposerPreferences(ctx context.Context, preferences []*gloas.SignedProposerPreferences) error
}

// GloasProposerCalls is the beacon-node surface for producing and publishing Gloas (ePBS) blocks
// (SIP #94 §4). go-eth2-client has no Gloas types, so these are hand-rolled over HTTP against the
// produceBlockV4 / publish endpoints (beacon-APIs#580, unmerged) — verify and iterate on a Gloas devnet.
type GloasProposerCalls interface {
	// GetGloasBeaconBlock produces a Gloas beacon block for the slot; the payload itself ships
	// separately in the §6 envelope, so the block carries only the execution-payload bid.
	GetGloasBeaconBlock(ctx context.Context, slot phase0.Slot, graffiti, randao []byte) (*gloas.BeaconBlock, error)
	// SubmitGloasBeaconBlock publishes a signed Gloas block.
	SubmitGloasBeaconBlock(ctx context.Context, block *gloas.SignedBeaconBlock) error
}

// GloasEnvelopeCalls is the beacon-node surface for the §6 execution-payload envelope (SIP #94 §6):
// fetching the payload the proposer committed to (self-build) and publishing the signed envelope. Like
// the block calls, these are hand-rolled over HTTP (beacon-APIs#580, unmerged) — verify on a Gloas devnet.
type GloasEnvelopeCalls interface {
	// GetExecutionPayloadEnvelope fetches the execution-payload envelope for the proposer's committed
	// block, to be blinded, agreed in §6 QBFT, and signed.
	GetExecutionPayloadEnvelope(ctx context.Context, slot phase0.Slot, beaconBlockRoot phase0.Root) (*gloas.ExecutionPayloadEnvelope, error)
	// SubmitExecutionPayloadEnvelope publishes the signed envelope.
	SubmitExecutionPayloadEnvelope(ctx context.Context, signed *gloas.SignedExecutionPayloadEnvelope) error
}
