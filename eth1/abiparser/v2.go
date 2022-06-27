package abiparser

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// MalformedEventError is returned when event is malformed
type MalformedEventError struct {
	Err error
}

func (e *MalformedEventError) Error() string {
	return e.Err.Error()
}

// Event names
const (
	OperatorAdded     = "OperatorAdded"
	OperatorRemoved   = "OperatorRemoved"
	ValidatorAdded    = "ValidatorAdded"
	ValidatorRemoved  = "ValidatorRemoved"
	AccountLiquidated = "AccountLiquidated"
	AccountEnabled    = "AccountEnabled"
)

// ValidatorAddedEvent struct represents event received by the smart contract
type ValidatorAddedEvent struct {
	PublicKey          []byte
	OwnerAddress       common.Address
	OperatorPublicKeys [][]byte
	OperatorIds        []*big.Int
	SharesPublicKeys   [][]byte
	EncryptedKeys      [][]byte
}

// AccountLiquidatedEvent struct represents event received by the smart contract
type AccountLiquidatedEvent struct {
	OwnerAddress common.Address
}

// AccountEnabledEvent struct represents event received by the smart contract
type AccountEnabledEvent struct {
	OwnerAddress common.Address
}

// OperatorAddedEvent struct represents event received by the smart contract
type OperatorAddedEvent struct {
	Id           *big.Int //nolint
	Name         string
	OwnerAddress common.Address
	PublicKey    []byte
	Fee          *big.Int
}

// OperatorRemovedEvent struct represents event received by the smart contract
type OperatorRemovedEvent struct {
	OperatorId   *big.Int //nolint
	OwnerAddress common.Address
}

// ValidatorRemovedEvent struct represents event received by the smart contract
type ValidatorRemovedEvent struct {
	OwnerAddress common.Address
	PublicKey    []byte
}

// AbiV2 parsing events from v2 abi contract
type AbiV2 struct {
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (v2 *AbiV2) ParseOperatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	topics []common.Hash,
	contractAbi abi.ABI,
) (*OperatorAddedEvent, error) {
	var operatorAddedEvent OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&operatorAddedEvent, OperatorAdded, data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrapf(err, "could not unpack %s event", OperatorAdded),
		}
	}
	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "could not define ABI")
	}
	pubKey, err := readOperatorPubKey(operatorAddedEvent.PublicKey, outAbi)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read %s event operator public key", OperatorAdded)
	}
	operatorAddedEvent.PublicKey = []byte(pubKey)

	if len(topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics. no owner address provided", OperatorAdded),
		}
	}
	operatorAddedEvent.OwnerAddress = common.HexToAddress(topics[1].Hex())
	return &operatorAddedEvent, nil
}

// ParseOperatorRemovedEvent parses OperatorRemovedEvent
func (v2 *AbiV2) ParseOperatorRemovedEvent(
	logger *zap.Logger,
	data []byte,
	topics []common.Hash,
	contractAbi abi.ABI,
) (*OperatorRemovedEvent, error) {
	var operatorRemovedEvent OperatorRemovedEvent
	err := contractAbi.UnpackIntoInterface(&operatorRemovedEvent, OperatorRemoved, data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrapf(err, "could not unpack %s event", OperatorRemoved),
		}
	}

	if len(topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics. no owner address provided", OperatorRemoved),
		}
	}
	operatorRemovedEvent.OwnerAddress = common.HexToAddress(topics[1].Hex())
	return &operatorRemovedEvent, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (v2 *AbiV2) ParseValidatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	contractAbi abi.ABI,
) (event *ValidatorAddedEvent, error error) {
	var validatorAddedEvent ValidatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&validatorAddedEvent, ValidatorAdded, data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrapf(err, "could not unpack %s event", ValidatorAdded),
		}
	}

	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "could not define ABI")
	}

	for i, ek := range validatorAddedEvent.EncryptedKeys {
		out, err := outAbi.Unpack("method", ek)
		if err != nil {
			return nil, &MalformedEventError{
				Err: errors.Wrapf(err, "could not unpack %s event EncryptedKey", ValidatorAdded),
			}
		}
		if encryptedSharePrivateKey, ok := out[0].(string); ok {
			validatorAddedEvent.EncryptedKeys[i] = []byte(encryptedSharePrivateKey)
		}
	}

	return &validatorAddedEvent, nil
}

// ParseValidatorRemovedEvent parses ValidatorRemovedEvent
func (v2 *AbiV2) ParseValidatorRemovedEvent(logger *zap.Logger, data []byte, contractAbi abi.ABI) (*ValidatorRemovedEvent, error) {
	var validatorRemovedEvent ValidatorRemovedEvent
	err := contractAbi.UnpackIntoInterface(&validatorRemovedEvent, ValidatorRemoved, data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrapf(err, "could not unpack %s event", ValidatorRemoved),
		}
	}

	return &validatorRemovedEvent, nil
}

// ParseAccountLiquidatedEvent parses AccountLiquidatedEvent
func (v2 *AbiV2) ParseAccountLiquidatedEvent(topics []common.Hash) (*AccountLiquidatedEvent, error) {
	var accountLiquidatedEvent AccountLiquidatedEvent

	if len(topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics. no owner address provided", AccountLiquidated),
		}
	}
	accountLiquidatedEvent.OwnerAddress = common.HexToAddress(topics[1].Hex())
	return &accountLiquidatedEvent, nil
}

// ParseAccountEnabledEvent parses AccountEnabledEvent
func (v2 *AbiV2) ParseAccountEnabledEvent(topics []common.Hash) (*AccountEnabledEvent, error) {
	var accountEnabledEvent AccountEnabledEvent

	if len(topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics. no owner address provided", AccountEnabled),
		}
	}
	accountEnabledEvent.OwnerAddress = common.HexToAddress(topics[1].Hex())
	return &accountEnabledEvent, nil
}
