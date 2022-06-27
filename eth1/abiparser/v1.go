package abiparser

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ValidatorAddedEventV1 struct represents event received by the smart contract
type ValidatorAddedEventV1 struct {
	PublicKey          []byte
	OwnerAddress       common.Address
	OperatorPublicKeys [][]byte
	SharesPublicKeys   [][]byte
	EncryptedKeys      [][]byte
}

// OperatorAddedEventV1 struct represents event received by the smart contract
type OperatorAddedEventV1 struct {
	Name         string
	OwnerAddress common.Address
	PublicKey    []byte
}

// AdapterV1 between v1 to v2 format
type AdapterV1 struct {
	abiV1 *AbiV1
}

// ParseOperatorAddedEvent parses OperatorAddedEventV1 to OperatorAddedEvent
func (a AdapterV1) ParseOperatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	topics []common.Hash,
	contractAbi abi.ABI,
) (*OperatorAddedEvent, error) {
	event, err := a.abiV1.ParseOperatorAddedEvent(logger, data, topics, contractAbi)
	if event == nil {
		return nil, err
	}
	// TODO: ID is missing
	return &OperatorAddedEvent{
		Name:         event.Name,
		PublicKey:    event.PublicKey,
		OwnerAddress: event.OwnerAddress,
	}, err
}

// ParseValidatorAddedEvent parses ValidatorAddedEventV1 to ValidatorAddedEvent
func (a AdapterV1) ParseValidatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	contractAbi abi.ABI,
) (*ValidatorAddedEvent, error) {
	event, err := a.abiV1.ParseValidatorAddedEvent(logger, data, contractAbi)
	if event == nil {
		return nil, err
	}

	// TODO: adjust to the new structure
	return &ValidatorAddedEvent{
		PublicKey:          event.PublicKey,
		OwnerAddress:       event.OwnerAddress,
		OperatorPublicKeys: event.OperatorPublicKeys,
		SharesPublicKeys:   event.SharesPublicKeys,
		EncryptedKeys:      event.EncryptedKeys,
	}, err
}

// ParseValidatorRemovedEvent event is not supported in v1 format
func (a AdapterV1) ParseValidatorRemovedEvent(logger *zap.Logger, data []byte, contractAbi abi.ABI) (*ValidatorRemovedEvent, error) {
	return nil, nil
}

// ParseOperatorRemovedEvent event is not supported in v1 format
func (a AdapterV1) ParseOperatorRemovedEvent(logger *zap.Logger, data []byte, topics []common.Hash, contractAbi abi.ABI) (*OperatorRemovedEvent, error) {
	return nil, nil
}

// ParseAccountLiquidatedEvent event is not supported in v1 format
func (a AdapterV1) ParseAccountLiquidatedEvent(topics []common.Hash) (*AccountLiquidatedEvent, error) {
	return nil, nil
}

// ParseAccountEnabledEvent event is not supported in v1 format
func (a AdapterV1) ParseAccountEnabledEvent(topics []common.Hash) (*AccountEnabledEvent, error) {
	return nil, nil
}

// AbiV1 parsing events from v1 abi contract
type AbiV1 struct {
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (v1 *AbiV1) ParseOperatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	topics []common.Hash,
	contractAbi abi.ABI,
) (*OperatorAddedEventV1, error) {
	var operatorAddedEvent OperatorAddedEventV1
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

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (v1 *AbiV1) ParseValidatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	contractAbi abi.ABI,
) (*ValidatorAddedEventV1, error) {
	var validatorAddedEvent ValidatorAddedEventV1
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

	for i, operatorPublicKey := range validatorAddedEvent.OperatorPublicKeys {
		operatorPublicKey, err := readOperatorPubKey(operatorPublicKey, outAbi)
		if err != nil {
			return nil, errors.Wrapf(err, "could not read %s event operator public key", ValidatorAdded)
		}
		validatorAddedEvent.OperatorPublicKeys[i] = []byte(operatorPublicKey) // set for further use in code

		out, err := outAbi.Unpack("method", validatorAddedEvent.EncryptedKeys[i])
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
