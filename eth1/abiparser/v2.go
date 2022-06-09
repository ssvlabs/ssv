package abiparser

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Event names
const (
	OperatorAdded     = "OperatorAdded"
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

// ValidatorRemovedEvent struct represents event received by the smart contract
type ValidatorRemovedEvent struct {
	OwnerAddress common.Address
	PublicKey    []byte
}

// AbiV2 parsing events from v2 abi contract
type AbiV2 struct {
}

// UnpackError is returned when unpacking fails
type UnpackError struct {
	Err error
}

func (e *UnpackError) Error() string {
	return e.Err.Error()
}

// DecryptError is returned when the decryption of the share private key fails
type DecryptError struct {
	Err error
}

func (e *DecryptError) Error() string {
	return e.Err.Error()
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
		return nil, &UnpackError{
			Err: errors.Wrap(err, "failed to unpack OperatorAdded event"),
		}
	}
	outAbi, err := getOutAbi()
	if err != nil {
		return nil, err
	}
	pubKey, err := readOperatorPubKey(operatorAddedEvent.PublicKey, outAbi)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read OperatorPublicKey")
	}
	operatorAddedEvent.PublicKey = []byte(pubKey)

	if len(topics) < 2 {
		return nil, errors.New("operator added event missing topics. no owner address provided")
	}
	operatorAddedEvent.OwnerAddress = common.HexToAddress(topics[1].Hex())
	return &operatorAddedEvent, nil
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
		return nil, &UnpackError{
			Err: errors.Wrapf(err, "Failed to unpack %s event", ValidatorAdded),
		}
	}

	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "failed to define ABI")
	}

	for i, ek := range validatorAddedEvent.EncryptedKeys {
		out, err := outAbi.Unpack("method", ek)
		if err != nil {
			return nil, &UnpackError{
				Err: errors.Wrap(err, "failed to unpack EncryptedKey"),
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
		return nil, &UnpackError{
			Err: errors.Wrap(err, "failed to unpack ValidatorRemoved event"),
		}
	}

	return &validatorRemovedEvent, nil
}

// ParseAccountLiquidatedEvent parses AccountLiquidatedEvent
func (v2 *AbiV2) ParseAccountLiquidatedEvent(topics []common.Hash) (*AccountLiquidatedEvent, error) {
	var accountLiquidatedEvent AccountLiquidatedEvent

	if len(topics) < 2 {
		return nil, errors.New("account liquidated event missing topics. no owner address provided")
	}
	accountLiquidatedEvent.OwnerAddress = common.HexToAddress(topics[1].Hex())
	return &accountLiquidatedEvent, nil
}

// ParseAccountEnabledEvent parses AccountEnabledEvent
func (v2 *AbiV2) ParseAccountEnabledEvent(topics []common.Hash) (*AccountEnabledEvent, error) {
	var accountEnabledEvent AccountEnabledEvent

	if len(topics) < 2 {
		return nil, errors.New("account enabled event missing topics. no owner address provided")
	}
	accountEnabledEvent.OwnerAddress = common.HexToAddress(topics[1].Hex())
	return &accountEnabledEvent, nil
}
