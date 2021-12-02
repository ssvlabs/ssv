package abiparser

import (
	"crypto/rsa"
	"encoding/hex"
	"github.com/bloxapp/ssv/utils/rsaencryption"
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

// LegacyValidatorAddedEvent struct represents event received by the smart contract
type LegacyValidatorAddedEvent struct {
	PublicKey    []byte
	OwnerAddress common.Address
	OessList     []Oess
}

// LegacyOperatorAddedEvent struct represents event received by the smart contract
type LegacyOperatorAddedEvent struct {
	Name           string
	PublicKey      []byte
	PaymentAddress common.Address
	OwnerAddress   common.Address
}

// LegacyAdapter between legacy to v2 format
type LegacyAdapter struct {
	legacyAbi *LegacyAbi
}

// ParseOperatorAddedEvent parses LegacyOperatorAddedEvent to OperatorAddedEvent
func (adapter LegacyAdapter) ParseOperatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*OperatorAddedEvent, bool, error) {
	event, isEventBelongsToOperator, err := adapter.legacyAbi.ParseOperatorAddedEvent(logger, operatorPrivateKey, data, contractAbi)
	if event == nil {
		return nil, isEventBelongsToOperator, err
	}
	return &OperatorAddedEvent{
		Name:         event.Name,
		PublicKey:    event.PublicKey,
		OwnerAddress: event.OwnerAddress,
	}, isEventBelongsToOperator, err
}

// ParseValidatorAddedEvent parses LegacyValidatorAddedEvent to ValidatorAddedEvent
func (adapter LegacyAdapter) ParseValidatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*ValidatorAddedEvent, bool, error) {
	event, isEventBelongsToOperator, err := adapter.legacyAbi.ParseValidatorAddedEvent(logger, operatorPrivateKey, data, contractAbi)
	if event == nil {
		return nil, isEventBelongsToOperator, err
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
	}, isEventBelongsToOperator, err
}

// LegacyAbi parsing events from legacy abi contract
type LegacyAbi struct {
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (l *LegacyAbi) ParseOperatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*LegacyOperatorAddedEvent, bool, error) {
	var operatorAddedEvent LegacyOperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&operatorAddedEvent, "OperatorAdded", data)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to unpack OperatorAdded event")
	}
	outAbi, err := getOutAbi()
	if err != nil {
		return nil, false, err
	}
	pubKey, err := readOperatorPubKey(operatorAddedEvent.PublicKey, outAbi)
	if err != nil {
		return nil, false, err
	}
	operatorAddedEvent.PublicKey = []byte(pubKey)
	logger.Debug("OperatorAdded Event",
		zap.String("Operator PublicKey", pubKey),
		zap.String("Payment Address", operatorAddedEvent.PaymentAddress.String()))
	var nodeOperatorPubKey string
	if operatorPrivateKey != nil {
		nodeOperatorPubKey, err = rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to extract public key")
		}
	}
	isEventBelongsToOperator := strings.EqualFold(pubKey, nodeOperatorPubKey)
	return &operatorAddedEvent, isEventBelongsToOperator, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (l *LegacyAbi) ParseValidatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*LegacyValidatorAddedEvent, bool, error) {
	var validatorAddedEvent LegacyValidatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&validatorAddedEvent, "ValidatorAdded", data)
	if err != nil {
		return nil, false, errors.Wrap(err, "Failed to unpack ValidatorAdded event")
	}

	logger.Debug("ValidatorAdded Event",
		zap.String("Validator PublicKey", hex.EncodeToString(validatorAddedEvent.PublicKey)),
		zap.String("Owner Address", validatorAddedEvent.OwnerAddress.String()))

	var isEventBelongsToOperator bool

	for i := range validatorAddedEvent.OessList {
		validatorShare := &validatorAddedEvent.OessList[i]

		outAbi, err := getOutAbi()
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to define ABI")
		}
		operatorPublicKey, err := readOperatorPubKey(validatorShare.OperatorPublicKey, outAbi)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to unpack OperatorPublicKey")
		}

		validatorShare.OperatorPublicKey = []byte(operatorPublicKey) // set for further use in code
		if operatorPrivateKey == nil {
			continue
		}
		nodeOperatorPubKey, err := rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to extract public key")
		}
		if strings.EqualFold(operatorPublicKey, nodeOperatorPubKey) {
			out, err := outAbi.Unpack("method", validatorShare.EncryptedKey)
			if err != nil {
				return nil, false, errors.Wrap(err, "failed to unpack EncryptedKey")
			}

			if encryptedSharePrivateKey, ok := out[0].(string); ok {
				decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, encryptedSharePrivateKey)
				decryptedSharePrivateKey = strings.Replace(decryptedSharePrivateKey, "0x", "", 1)
				if err != nil {
					return nil, false, errors.Wrap(err, "failed to decrypt share private key")
				}
				validatorShare.EncryptedKey = []byte(decryptedSharePrivateKey)
				isEventBelongsToOperator = true
			}
		}
	}

	return &validatorAddedEvent, isEventBelongsToOperator, nil
}

func readOperatorPubKey(operatorPublicKey []byte, outAbi abi.ABI) (string, error) {
	outOperatorPublicKey, err := outAbi.Unpack("method", operatorPublicKey)
	if err != nil {
		return "", errors.Wrap(err, "failed to unpack OperatorPublicKey")
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
