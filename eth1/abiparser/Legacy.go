package abiparser

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math/big"
	"strings"
)

// Oess struct stands for operator encrypted secret share
type Oess struct {
	Index             *big.Int
	OperatorPublicKey []byte
	SharedPublicKey   []byte
	EncryptedKey      []byte
}

// ValidatorAddedEventLegacy struct represents event received by the smart contract
type ValidatorAddedEventLegacy struct {
	PublicKey    []byte
	OwnerAddress common.Address
	OessList     []Oess
}

// OperatorAddedEventLegacy struct represents event received by the smart contract
type OperatorAddedEventLegacy struct {
	Name           string
	PublicKey      []byte
	PaymentAddress common.Address
	OwnerAddress   common.Address
}

// AdapterLegacy between legacy to v1 format
type AdapterLegacy struct {
	legacyAbi *AbiLegacy
}

// ParseOperatorAddedEvent parses OperatorAddedEventLegacy to OperatorAddedEvent
func (a AdapterLegacy) ParseOperatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	topics []common.Hash,
	contractAbi abi.ABI,
) (*OperatorAddedEvent, error) {
	event, err := a.legacyAbi.ParseOperatorAddedEvent(logger, data, contractAbi)
	if event == nil {
		return nil, err
	}
	return &OperatorAddedEvent{
		Name:         event.Name,
		PublicKey:    event.PublicKey,
		OwnerAddress: event.OwnerAddress,
	}, err
}

// ParseValidatorAddedEvent parses ValidatorAddedEventLegacy to ValidatorAddedEvent
func (a AdapterLegacy) ParseValidatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	contractAbi abi.ABI,
) (*ValidatorAddedEvent, error) {
	event, err := a.legacyAbi.ParseValidatorAddedEvent(logger, data, contractAbi)
	if event == nil {
		return nil, err
	}

	unPackOess := func(oesses []Oess) ([][]byte, [][]byte, [][]byte) {
		var operatorPublicKeys, sharesPublicKeys, encryptedKeys [][]byte
		for _, oess := range oesses {
			operatorPublicKeys = append(operatorPublicKeys, oess.OperatorPublicKey)
			sharesPublicKeys = append(sharesPublicKeys, oess.SharedPublicKey)
			encryptedKeys = append(encryptedKeys, oess.EncryptedKey)
		}
		return operatorPublicKeys, sharesPublicKeys, encryptedKeys
	}

	operatorPublicKeys, sharesPublicKeys, encryptedKeys := unPackOess(event.OessList)
	return &ValidatorAddedEvent{
		PublicKey:          event.PublicKey,
		OwnerAddress:       event.OwnerAddress,
		OperatorPublicKeys: operatorPublicKeys,
		SharesPublicKeys:   sharesPublicKeys,
		EncryptedKeys:      encryptedKeys,
	}, err
}

// ParseValidatorUpdatedEvent event is not supported in legacy format
func (a AdapterLegacy) ParseValidatorUpdatedEvent(logger *zap.Logger, data []byte, contractAbi abi.ABI) (*ValidatorAddedEvent, error) {
	return nil, nil
}

// ParseValidatorRemovedEvent event is not supported in legacy format
func (a AdapterLegacy) ParseValidatorRemovedEvent(logger *zap.Logger, data []byte, contractAbi abi.ABI) (*ValidatorRemovedEvent, error) {
	return nil, nil
}

// ParseAccountLiquidatedEvent event is not supported in legacy format
func (a AdapterLegacy) ParseAccountLiquidatedEvent(logger *zap.Logger, data []byte, contractAbi abi.ABI) (*AccountLiquidatedEvent, error) {
	return nil, nil
}

// ParseAccountEnabledEvent event is not supported in legacy format
func (a AdapterLegacy) ParseAccountEnabledEvent(logger *zap.Logger, data []byte, contractAbi abi.ABI) (*AccountEnabledEvent, error) {
	return nil, nil
}

// AbiLegacy parsing events from legacy abi contract
type AbiLegacy struct {
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (a *AbiLegacy) ParseOperatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	contractAbi abi.ABI,
) (*OperatorAddedEventLegacy, error) {
	var operatorAddedEvent OperatorAddedEventLegacy
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
		return nil, err
	}
	operatorAddedEvent.PublicKey = []byte(pubKey)
	return &operatorAddedEvent, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (a *AbiLegacy) ParseValidatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	contractAbi abi.ABI,
) (*ValidatorAddedEventLegacy, error) {
	var validatorAddedEvent ValidatorAddedEventLegacy
	err := contractAbi.UnpackIntoInterface(&validatorAddedEvent, ValidatorAdded, data)
	if err != nil {
		return nil, &UnpackError{
			Err: errors.Wrap(err, "failed to unpack ValidatorAdded event"),
		}
	}

	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "failed to define ABI")
	}

	for i := range validatorAddedEvent.OessList {
		validatorShare := &validatorAddedEvent.OessList[i]

		operatorPublicKey, err := readOperatorPubKey(validatorShare.OperatorPublicKey, outAbi)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read OperatorPublicKey")
		}
		validatorShare.OperatorPublicKey = []byte(operatorPublicKey) // set for further use in code

		out, err := outAbi.Unpack("method", validatorShare.EncryptedKey)
		if err != nil {
			return nil, errors.Wrap(err, "failed to unpack EncryptedKey")
		}
		if encryptedSharePrivateKey, ok := out[0].(string); ok {
			validatorShare.EncryptedKey = []byte(encryptedSharePrivateKey)
		}
	}

	return &validatorAddedEvent, nil
}

func readOperatorPubKey(operatorPublicKey []byte, outAbi abi.ABI) (string, error) {
	outOperatorPublicKey, err := outAbi.Unpack("method", operatorPublicKey)
	if err != nil {
		return "", &UnpackError{
			Err: err,
		}
	}

	if operatorPublicKey, ok := outOperatorPublicKey[0].(string); ok {
		return operatorPublicKey, nil
	}

	return "", errors.Wrap(err, "failed to read OperatorPublicKey")
}

func getOutAbi() (abi.ABI, error) {
	def := `[{ "name" : "method", "type": "function", "outputs": [{"type": "string"}]}]`
	return abi.JSON(strings.NewReader(def))
}
