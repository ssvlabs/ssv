package beacon

import (
	"context"

	api "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/storage/basedb"
)

//go:generate mockgen -package=beacon -destination=./mock_client.go -source=./client.go

// Beacon represents the behavior of the beacon node connector
type Beacon interface {
	SigningUtil

	// GetDuties returns duties for the passed validators indices
	GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*spectypes.Duty, error)

	// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
	GetValidatorData(validatorPubKeys []spec.BLSPubKey) (map[spec.ValidatorIndex]*api.Validator, error)

	// GetAttestationData returns attestation data by the given slot and committee index
	GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error)

	// SubmitAttestation submit the attestation to the node
	SubmitAttestation(attestation *spec.Attestation) error

	// SubscribeToCommitteeSubnet subscribe committee to subnet (p2p topic)
	SubscribeToCommitteeSubnet(subscription []*api.BeaconCommitteeSubscription) error
}

// SigningUtil is an interface for beacon node signing specific methods
type SigningUtil interface {
	GetDomain(data *spec.AttestationData) ([]byte, error)
	ComputeSigningRoot(object interface{}, domain []byte) ([32]byte, error)
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
