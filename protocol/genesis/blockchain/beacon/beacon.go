package genesisbeacon

import (
	"context"

	eth2client "github.com/attestantio/go-eth2-client"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspecssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
)

// TODO: add missing tests

//go:generate mockgen -package=genesisbeacon -destination=./mock_client.go -source=./beacon.go

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

type BeaconNode interface {
	genesisspecssv.BeaconNode // spec beacon interface
	beaconDuties
	beaconSubscriber
	beaconValidator
	signer // TODO need to handle differently
	proposer
}
