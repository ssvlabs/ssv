package abiparser

import (
	"crypto/rsa"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
	"math/big"
)

// DistributedKeyRequestedEvent represents a DistributedKeyRequested event raised by the smart contract.
type DistributedKeyRequestedEvent struct {
	RequestId    *big.Int
	OwnerAddress common.Address
	OperatorIds  []*big.Int
}

// V3Abi parsing events from v3 abi contract
type V3Abi struct {
	v2 *V2Abi
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (v3 *V3Abi) ParseOperatorAddedEvent(
	logger *zap.Logger,
	operatorPubKey string,
	data []byte,
	topics []common.Hash,
	contractAbi abi.ABI,
) (*OperatorAddedEvent, bool, bool, error) {
	return v3.getV2().ParseOperatorAddedEvent(logger, operatorPubKey, data, topics, contractAbi)
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (v3 *V3Abi) ParseValidatorAddedEvent(
	logger *zap.Logger,
	operatorPrivateKey *rsa.PrivateKey,
	data []byte,
	contractAbi abi.ABI,
) (*ValidatorAddedEvent, bool, bool, error) {
	return v3.getV2().ParseValidatorAddedEvent(logger, operatorPrivateKey, data, contractAbi)
}

// ParseDistributedKeyRequestedEvent parses an DistributedKeyRequestedEvent
func (v3 *V3Abi) ParseDistributedKeyRequestedEvent(
	logger *zap.Logger,
	operatorPubKey string,
	data []byte,
	topics []common.Hash,
	contractAbi abi.ABI,
) (*DistributedKeyRequestedEvent, bool, bool, error) {
	// TODO<MPC>: Implement
	return nil, false, false, nil
}

func (v3 *V3Abi) getV2() *V2Abi {
	if v3.v2 == nil {
		v3.v2 = &V2Abi{}
	}
	return v3.v2
}
