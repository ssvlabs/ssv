package validation

import (
	spectypes "github.com/bloxapp/ssv-spec/alan/types"

	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

type OperatorStore interface {
	GetOperatorData(id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error)
}
