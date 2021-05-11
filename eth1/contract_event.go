package eth1

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/pubsub"
)

// ContractEvent struct
type ContractEvent struct {
	pubsub.BaseSubject
	Log  types.Log
	Data interface{}
}

// Oess struct
type Oess struct {
	OperatorPubKey []byte
	Index          *big.Int
	SharePubKey    []byte
	EncryptedKey   []byte
}

// ValidatorAddedEvent struct
type ValidatorAddedEvent struct {
	Pubkey       []byte
	OwnerAddress common.Address
	Oess         []byte
}

// OperatorAddedEvent struct
type OperatorAddedEvent struct {
	Name           string
	Pubkey         []byte
	PaymentAddress common.Address
}

// ValidatorEvent struct
type ValidatorEvent struct {
	ValidatorPubKey []byte
	OessList        []Oess
}

// NewContractEvent create new event subject
func NewContractEvent(name string) *ContractEvent {
	return &ContractEvent{
		BaseSubject: pubsub.BaseSubject{
			Name: name,
		},
	}
}

// NotifyAll notify all subscribe observables
func (e *ContractEvent) NotifyAll() {
	for _, observer := range e.ObserverList {
		observer.InformObserver(e.Data)
	}
}
