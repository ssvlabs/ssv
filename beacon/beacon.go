package beacon

import (
	"context"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/basedb"
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
	DB             basedb.IDb
}

// Beacon represents the behavior of the beacon node connector
type Beacon interface {
	KeyManager
	SigningUtil

	// ExtendIndexMap extanding the pubkeys map of the client (in order to prevent redundant call to fetch pubkeys from node)
	ExtendIndexMap(index spec.ValidatorIndex, pubKey spec.BLSPubKey)

	// GetDuties returns duties for the passed validators indices
	GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*Duty, error)

	// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
	GetValidatorData(validatorPubKeys []spec.BLSPubKey) (map[spec.ValidatorIndex]*api.Validator, error)

	// GetAttestationData returns attestation data by the given slot and committee index
	GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error)

	// SubmitAttestation submit the attestation to the node
	SubmitAttestation(attestation *spec.Attestation) error

	// SubscribeToCommitteeSubnet subscribe committee to subnet (p2p topic)
	SubscribeToCommitteeSubnet(subscription []*api.BeaconCommitteeSubscription) error
}

// KeyManager is an interface responsible for all key manager functions
type KeyManager interface {
	Signer
	// AddShare saves a share key
	AddShare(shareKey *bls.SecretKey) error
}

// Signer is an interface responsible for all signing operations
type Signer interface {
	SlashingProtection
	// SignIBFTMessage signs a network iBFT msg
	SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error)
	// SignAttestation signs the given attestation
	SignAttestation(data *spec.AttestationData, duty *Duty, pk []byte) (*spec.Attestation, []byte, error)
}

// SlashingProtection is an interface for all signing slashing protection
type SlashingProtection interface {
	IsAttestationSlashable(data *spec.AttestationData, pk []byte) error
}

// SigningUtil is an interface for beacon node signing specific methods
type SigningUtil interface {
	GetDomain(data *spec.AttestationData) ([]byte, error)
	ComputeSigningRoot(object interface{}, domain []byte) ([32]byte, error)
}
