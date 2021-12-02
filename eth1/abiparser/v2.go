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

type v2Abi struct {
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (v2 *v2Abi) ParseOperatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*OperatorAddedEvent, bool, error) {
	var operatorAddedEvent OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&operatorAddedEvent, "OperatorAdded", data)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to unpack OperatorAdded event")
	}

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
func (v2 *v2Abi) ParseValidatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*ValidatorAddedEvent, bool, error) {
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

		operatorPublicKey = []byte(hexOperatorPublicKey) // set for further use in code
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

type V2Adapter struct {
	v2Abi *v2Abi
}

func (adapter V2Adapter) ParseOperatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*LegacyOperatorAddedEvent, bool, error) {
	event, isEventBelongsToOperator, err := adapter.v2Abi.ParseOperatorAddedEvent(logger, operatorPrivateKey, data, contractAbi)
	if event == nil {
		return nil, isEventBelongsToOperator, err
	}
	return &LegacyOperatorAddedEvent{
		Name:           event.Name,
		PublicKey:      event.PublicKey,
		PaymentAddress: event.OwnerAddress,
		OwnerAddress:   event.OwnerAddress,
	}, isEventBelongsToOperator, err
}

func (adapter V2Adapter) ParseValidatorAddedEvent(logger *zap.Logger, operatorPrivateKey *rsa.PrivateKey, data []byte, contractAbi abi.ABI) (*LegacyValidatorAddedEvent, bool, error) {
	event, isEventBelongsToOperator, err := adapter.v2Abi.ParseValidatorAddedEvent(logger, operatorPrivateKey, data, contractAbi)
	if event == nil {
		return nil, isEventBelongsToOperator, err
	}

	toOess := func(e *ValidatorAddedEvent) []Oess {
		var res []Oess
		for i, publicKey := range e.OperatorPublicKeys {
			res = append(res, Oess{
				Index:             big.NewInt(int64(i) + 1),
				OperatorPublicKey: publicKey,
				SharedPublicKey:   e.SharesPublicKeys[i],
				EncryptedKey:      e.EncryptedKeys[i],
			})
		}
		return res
	}

	return &LegacyValidatorAddedEvent{
		PublicKey:    event.PublicKey,
		OwnerAddress: event.OwnerAddress,
		OessList:     toOess(event),
	}, isEventBelongsToOperator, err
}
