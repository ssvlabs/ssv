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
		Id:           big.NewInt(0),
		Name:         event.Name,
		PublicKey:    event.PublicKey,
		OwnerAddress: event.OwnerAddress,
		Fee:          big.NewInt(0),
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

// ParseValidatorRemovedEvent event is not supported in legacy format
func (a AdapterLegacy) ParseValidatorRemovedEvent(logger *zap.Logger, data []byte, contractAbi abi.ABI) (*ValidatorRemovedEvent, error) {
	return nil, nil
}

// ParseOperatorRemovedEvent event is not supported in legacy format
func (a AdapterLegacy) ParseOperatorRemovedEvent(logger *zap.Logger, data []byte, topics []common.Hash, contractAbi abi.ABI) (*OperatorRemovedEvent, error) {
	return nil, nil
}

// ParseAccountLiquidatedEvent event is not supported in legacy format
func (a AdapterLegacy) ParseAccountLiquidatedEvent(topics []common.Hash) (*AccountLiquidatedEvent, error) {
	return nil, nil
}

// ParseAccountEnabledEvent event is not supported in legacy format
func (a AdapterLegacy) ParseAccountEnabledEvent(topics []common.Hash) (*AccountEnabledEvent, error) {
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
		return nil, &MalformedEventError{
			Err: errors.Wrapf(err, "could not unpack %s event", ValidatorAdded),
		}
	}

	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "could not define ABI")
	}

	for i := range validatorAddedEvent.OessList {
		validatorShare := &validatorAddedEvent.OessList[i]

		operatorPublicKey, err := readOperatorPubKey(validatorShare.OperatorPublicKey, outAbi)
		if err != nil {
			return nil, errors.Wrapf(err, "could not read %s event operator public key", ValidatorAdded)
		}
		validatorShare.OperatorPublicKey = []byte(operatorPublicKey) // set for further use in code

		out, err := outAbi.Unpack("method", validatorShare.EncryptedKey)
		if err != nil {
			return nil, &MalformedEventError{
				Err: errors.Wrapf(err, "could not unpack %s event EncryptedKey", ValidatorAdded),
			}
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
		return "", &MalformedEventError{
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
