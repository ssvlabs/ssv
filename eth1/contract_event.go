package eth1

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/pubsub"
)

// ContractEvent struct is an implementation of BaseSubject that notify about an event from the smart contract to all the registered observers
type ContractEvent struct {
	pubsub.BaseSubject
	Log  types.Log
	Data interface{}
}

// Oess struct stands for operator encrypted secret share
type Oess struct {
	Index             *big.Int
	OperatorPublicKey []byte
	SharedPublicKey   []byte
	EncryptedKey      []byte
}

// ValidatorAddedEvent struct represents event received by the smart contract
type ValidatorAddedEvent struct {
	PublicKey    []byte
	OwnerAddress common.Address
	OessList     []Oess
}

// OperatorAddedEvent struct represents event received by the smart contract
type OperatorAddedEvent struct {
	Name           string
	Pubkey         []byte
	PaymentAddress common.Address
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
