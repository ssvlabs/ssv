package beacon

import (
	"context"

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

// BeaconNode interface for all beacon duty calls
type BeaconNode interface {
	specssv.BeaconNode // spec beacon interface
	beaconDuties
	beaconSubscriber
	beaconValidator
	signer // TODO need to handle differently
	proposer
}
