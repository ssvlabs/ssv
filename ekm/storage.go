package ekm

import (
	"github.com/bloxapp/eth2-key-manager/core"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	registry "github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
)

const (
	prefix = "signer_data-"
)

// Storage represents the interface for ssv node storage
type Storage interface {
	registry.RegistryStore
	core.Storage
	core.SlashingStore

	RemoveHighestAttestation(pubKey []byte) error
	RemoveHighestProposal(pubKey []byte) error
	SetEncryptionKey(newKey string) error
	ListAccountsTxn(r basedb.Reader) ([]core.ValidatorAccount, error)
	SaveAccountTxn(rw basedb.ReadWriter, account core.ValidatorAccount) error
}

type storage struct {
	signerStorage
	spStorage
}

func NewEKMStorage(db, spDB basedb.Database, network spectypes.BeaconNetwork, logger *zap.Logger) Storage {
	storagePrefix := []byte(network)
	return &storage{
		signerStorage: newSignerStorage(db, logger, network, storagePrefix),
		spStorage:     newSlashingProtectionStorage(spDB, logger, storagePrefix),
	}
}
