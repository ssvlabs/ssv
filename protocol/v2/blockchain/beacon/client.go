package beacon

import (
	"context"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	eth2client "github.com/attestantio/go-eth2-client"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
)

// TODO: add missing tests

//go:generate mockgen -package=beacon -destination=./mock_client.go -source=./client.go

// beaconDuties interface serves all duty related calls
type beaconDuties interface {
	AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error)
	ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error)
	SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error)
	eth2client.EventsProvider
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
	GetValidatorData(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error)
}

type proposer interface {
	// SubmitProposalPreparation with fee recipients
	SubmitProposalPreparation(feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error
}

// TODO need to handle differently (by spec)
type signer interface {
	ComputeSigningRoot(object interface{}, domain phase0.Domain) ([32]byte, error)
}

// TODO: remove temp spec intefaces once spec is settled

// ProposerCalls interface has all block proposer duty specific calls
type TempSpecProposerCalls interface {
	// GetBeaconBlock returns beacon block by the given slot, graffiti, and randao.
	GetBeaconBlock(slot phase0.Slot, graffiti, randao []byte) (ssz.Marshaler, spec.DataVersion, error)
	// GetBlindedBeaconBlock returns blinded beacon block by the given slot, graffiti, and randao.
	//GetBlindedBeaconBlock(slot phase0.Slot, graffiti, randao []byte) (ssz.Marshaler, spec.DataVersion, error)
	// SubmitBeaconBlock submit the block to the node
	SubmitBeaconBlock(block *api.VersionedProposal, sig phase0.BLSSignature) error
	// SubmitBlindedBeaconBlock submit the blinded block to the node
	SubmitBlindedBeaconBlock(block *api.VersionedBlindedProposal, sig phase0.BLSSignature) error
}

// AttesterCalls interface has all attester duty specific calls
type TempSpecAttesterCalls interface {
	// GetAttestationData returns attestation data by the given slot and committee index
	GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (*phase0.AttestationData, spec.DataVersion, error)
	// SubmitAttestation submit the attestation to the node
	SubmitAttestations(attestations []*phase0.Attestation) error
}

// SyncCommitteeCalls interface has all sync committee duty specific calls
type TempSpecSyncCommitteeCalls interface {
	// GetSyncMessageBlockRoot returns beacon block root for sync committee
	GetSyncMessageBlockRoot(slot phase0.Slot) (phase0.Root, spec.DataVersion, error)
	// SubmitSyncMessage submits a signed sync committee msg
	SubmitSyncMessages(msg []*altair.SyncCommitteeMessage) error
}

type TempSpecBeaconNode interface {
	// GetBeaconNetwork returns the beacon network the node is on
	GetBeaconNetwork() spectypes.BeaconNetwork
	TempSpecAttesterCalls
	TempSpecProposerCalls
	specssv.AggregatorCalls
	TempSpecSyncCommitteeCalls
	specssv.SyncCommitteeContributionCalls
	specssv.ValidatorRegistrationCalls
	specssv.VoluntaryExitCalls
	specssv.DomainCalls
}

// BeaconNode interface for all beacon duty calls
type BeaconNode interface {
	TempSpecBeaconNode // spec beacon interface
	beaconDuties
	beaconSubscriber
	beaconValidator
	signer // TODO need to handle differently
	proposer
}

// Options for controller struct creation
type Options struct {
	Context        context.Context
	Network        Network
	BeaconNodeAddr string `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true"`
	Graffiti       []byte
	GasLimit       uint64
	CommonTimeout  time.Duration // Optional.
	LongTimeout    time.Duration // Optional.
}
