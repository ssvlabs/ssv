package abiparser

import (
	"crypto/rsa"
	"encoding/hex"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"strings"
)

// ValidatorAddedEvent struct represents event received by the smart contract
type ValidatorAddedEvent struct {
	PublicKey          []byte
	OwnerAddress       common.Address
	OperatorPublicKeys [][]byte
	SharesPublicKeys   [][]byte
	EncryptedKeys      [][]byte
}

// OperatorAddedEvent struct represents event received by the smart contract
type OperatorAddedEvent struct {
	Name         string
	PublicKey    []byte
	OwnerAddress common.Address
}

type V2Abi struct {
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (v2 *V2Abi) ParseOperatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*OperatorAddedEvent, bool, error) {
	var operatorAddedEvent OperatorAddedEvent
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
	hexPubkey := hex.EncodeToString(operatorAddedEvent.PublicKey)
	logger.Debug("OperatorAdded Event",
		zap.String("Operator PublicKey", hexPubkey),
		zap.String("Payment Address", operatorAddedEvent.OwnerAddress.String()))
	var nodeOperatorPubKey string
	if operatorPrivateKey != nil {
		nodeOperatorPubKey, err = rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to extract public key")
		}
	}
	isEventBelongsToOperator := strings.EqualFold(hexPubkey, nodeOperatorPubKey)
	return &operatorAddedEvent, isEventBelongsToOperator, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (v2 *V2Abi) ParseValidatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*ValidatorAddedEvent, bool, error) {
	var validatorAddedEvent ValidatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&validatorAddedEvent, "ValidatorAdded", data)
	if err != nil {
		return nil, false, errors.Wrap(err, "Failed to unpack ValidatorAdded event")
	}

	logger.Debug("ValidatorAdded Event",
		zap.String("Validator PublicKey", hex.EncodeToString(validatorAddedEvent.PublicKey)),
		zap.String("Owner Address", validatorAddedEvent.OwnerAddress.String()))

	var isEventBelongsToOperator bool

	for i, operatorPublicKey := range validatorAddedEvent.OperatorPublicKeys {
		outAbi, err := getOutAbi()
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to define ABI")
		}
		hexOperatorPublicKey, err := readOperatorPubKey(operatorPublicKey, outAbi)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to unpack OperatorPublicKey")
		}

		validatorAddedEvent.OperatorPublicKeys[i] = []byte(hexOperatorPublicKey) // set for further use in code
		if operatorPrivateKey == nil {
			continue
		}
		nodeOperatorPubKey, err := rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to extract public key")
		}
		if strings.EqualFold(hexOperatorPublicKey, nodeOperatorPubKey) {
			out, err := outAbi.Unpack("method", validatorAddedEvent.EncryptedKeys[i])
			if err != nil {
				return nil, false, errors.Wrap(err, "failed to unpack EncryptedKey")
			}

			if encryptedSharePrivateKey, ok := out[0].(string); ok {
				decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, encryptedSharePrivateKey)
				decryptedSharePrivateKey = strings.Replace(decryptedSharePrivateKey, "0x", "", 1)
				if err != nil {
					return nil, false, errors.Wrap(err, "failed to decrypt share private key")
				}
				validatorAddedEvent.EncryptedKeys[i] = []byte(decryptedSharePrivateKey)
				isEventBelongsToOperator = true
			}
		}
	}

	return &validatorAddedEvent, isEventBelongsToOperator, nil
}
