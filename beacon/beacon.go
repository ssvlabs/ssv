package beacon

import (
	"context"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	api "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
)

// Options for controller struct creation
type Options struct {
	Context        context.Context
	Logger         *zap.Logger
	Network        string `yaml:"Network" env:"NETWORK" env-default:"prater"`
	BeaconNodeAddr string `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true"`
	Graffiti       []byte
}

// Beacon represents the behavior of the beacon node connector
type Beacon interface {

	// ExtendIndexMap extanding the pubkeys map of the client (in order to prevent redundant call to fetch pubkeys from node)
	ExtendIndexMap(index spec.ValidatorIndex, pubKey spec.BLSPubKey)

	// GetDuties returns duties for the passed validators indices
	GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*Duty, error)

	// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
	GetValidatorData(validatorPubKeys []spec.BLSPubKey) (map[spec.ValidatorIndex]*api.Validator, error)

	// GetAttestationData returns attestation data by the given slot and committee index
	GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error)

	// SignAttestation signs the given attestation
	SignAttestation(data *spec.AttestationData, duty *Duty, shareKey *bls.SecretKey) (*spec.Attestation, []byte, error)

	// SubmitAttestation submit the attestation to the node
	SubmitAttestation(attestation *spec.Attestation) error

	// SubscribeToCommitteeSubnet subscribe committee to subnet (p2p topic)
	SubscribeToCommitteeSubnet(subscription []*api.BeaconCommitteeSubscription) error
}
