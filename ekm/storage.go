package ekm

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/storage/basedb"
)

const (
	prefix = "signer_data-"
)

// Storage represents the interface for ssv node storage
type Storage interface {
	SignerStorage
	SpStorage
}

type storage struct {
	SignerStorage
	SpStorage
}

func NewEKMStorage(db, spDB basedb.Database, network spectypes.BeaconNetwork, logger *zap.Logger) Storage {
	storagePrefix := []byte(network)
	return &storage{
		NewSignerStorage(db, logger, network, storagePrefix),
		NewSlashingProtectionStorage(spDB, logger, storagePrefix),
	}
}
