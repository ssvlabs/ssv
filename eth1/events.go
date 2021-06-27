package eth1

import (
	"crypto/rsa"
	"encoding/hex"
	"github.com/bloxapp/ssv/shared/params"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

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
	PublicKey      []byte
	PaymentAddress common.Address
	OwnerAddress   common.Address
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func ParseOperatorAddedEvent(logger *zap.Logger, data []byte, contractAbi abi.ABI) (*OperatorAddedEvent, bool, error) {
	var operatorAddedEvent OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&operatorAddedEvent, "OperatorAdded", data)
	if err != nil {
		return nil, false, errors.Wrap(err, "Failed to unpack OperatorAdded event")
	}
	operatorPubkeyHex := hex.EncodeToString(operatorAddedEvent.PublicKey)
	logger.Debug("OperatorAdded Event",
		zap.String("Operator PublicKey", operatorPubkeyHex),
		zap.String("Payment Address", operatorAddedEvent.PaymentAddress.String()))
	isEventBelongsToOperator := strings.EqualFold(operatorPubkeyHex, params.SsvConfig().OperatorPublicKey)
	return &operatorAddedEvent, isEventBelongsToOperator, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func ParseValidatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*ValidatorAddedEvent, bool, error) {
	var validatorAddedEvent ValidatorAddedEvent
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

		def := `[{ "name" : "method", "type": "function", "outputs": [{"type": "string"}]}]` //TODO need to set as var?
		outAbi, err := abi.JSON(strings.NewReader(def))
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to define ABI")
		}

		outOperatorPublicKey, err := outAbi.Unpack("method", validatorShare.OperatorPublicKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to unpack OperatorPublicKey")
		}

		if operatorPublicKey, ok := outOperatorPublicKey[0].(string); ok {
			validatorShare.OperatorPublicKey = []byte(operatorPublicKey) // set for further use in code
			if strings.EqualFold(operatorPublicKey, params.SsvConfig().OperatorPublicKey) {
				if operatorPrivateKey == nil {
					continue
				}

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
	}

	return &validatorAddedEvent, isEventBelongsToOperator, nil
}
