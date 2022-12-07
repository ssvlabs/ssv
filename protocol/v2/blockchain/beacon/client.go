package beacon

import (
	"context"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/storage/basedb"
)

// TODO: add missing tests

//go:generate mockgen -package=beacon -destination=./mock_client.go -source=./client.go

// beaconDuties interface serves all duty related calls
type beaconDuties interface {
	// GetDuties returns duties for the passed validators indices
	GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*spectypes.Duty, error)
}

// beaconSubscriber interface serves all committee subscribe to subnet (p2p topic)
type beaconSubscriber interface {
	// SubscribeToCommitteeSubnet subscribe committee to subnet
	SubscribeToCommitteeSubnet(subscription []*eth2apiv1.BeaconCommitteeSubscription) error
	// SubmitSyncCommitteeSubscriptions subscribe to sync committee subnet
	SubmitSyncCommitteeSubscriptions(subscription []*eth2apiv1.SyncCommitteeSubscription) error
}

type beaconValidator interface {
	// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
	GetValidatorData(validatorPubKeys []spec.BLSPubKey) (map[spec.ValidatorIndex]*eth2apiv1.Validator, error)
}

// Beacon interface for all beacon duty calls
type Beacon interface {
	ssv.BeaconNode // spec beacon interface
	beaconDuties
	beaconSubscriber
	beaconValidator
}

// Options for controller struct creation
type Options struct {
	Context        context.Context
	Logger         *zap.Logger
	Network        string `yaml:"Network" env:"NETWORK" env-default:"prater"`
	BeaconNodeAddr string `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true"`
	Graffiti       []byte
	DB             basedb.IDb
}
