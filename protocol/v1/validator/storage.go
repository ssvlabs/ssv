package validator

import (
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/eth1"
)

// ICollection interface for validator storage
type ICollection interface {
	eth1.RegistryStore

	SaveValidatorShare(share *beacon.Share) error
	GetValidatorShare(key []byte) (*beacon.Share, bool, error)
	GetAllValidatorShares() ([]*beacon.Share, error)
	GetOperatorValidatorShares(operatorPubKey string, enabled bool) ([]*beacon.Share, error)
	GetValidatorSharesByOwnerAddress(ownerAddress string) ([]*beacon.Share, error)
	DeleteValidatorShare(key []byte) error
}
