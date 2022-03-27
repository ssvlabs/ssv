package abiparser

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math/big"
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

// OperatorAddedEvent struct represents event received by the smart contract
type OperatorAddedEvent struct {
	ID           *big.Int
	Name         string
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
) (*OperatorAddedEvent, bool, error) {
	var operatorAddedEvent OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&operatorAddedEvent, "OperatorAdded", data)
	if err != nil {
		return nil, true, errors.Wrap(err, "failed to unpack OperatorAdded event")
	}
	outAbi, err := getOutAbi()
	if err != nil {
		return nil, false, err
	}
	pubKey, err := readOperatorPubKey(operatorAddedEvent.PublicKey, outAbi)
	if err != nil {
		return nil, true, errors.Wrap(err, "failed to read OperatorPublicKey")
	}
	operatorAddedEvent.PublicKey = []byte(pubKey)

	if len(topics) > 1 {
		operatorAddedEvent.OwnerAddress = common.HexToAddress(topics[1].Hex())
	} else {
		logger.Error("operator event missing topics. no owner address provided.")
	}
	return &operatorAddedEvent, false, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (v2 *AbiV2) ParseValidatorAddedEvent(
	logger *zap.Logger,
	data []byte,
	contractAbi abi.ABI,
) (event *ValidatorAddedEvent, unpackErr bool, error error) {
	var validatorAddedEvent ValidatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&validatorAddedEvent, "ValidatorAdded", data)
	if err != nil {
		return nil, true, errors.Wrap(err, "Failed to unpack ValidatorAdded event")
	}

	outAbi, err := getOutAbi()
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to define ABI")
	}

	for i, ek := range validatorAddedEvent.EncryptedKeys {
		out, err := outAbi.Unpack("method", ek)
		if err != nil {
			return nil, true, errors.Wrap(err, "failed to unpack EncryptedKey")
		}
		if encryptedSharePrivateKey, ok := out[0].(string); ok {
			validatorAddedEvent.EncryptedKeys[i] = []byte(encryptedSharePrivateKey)
		}
	}

	return &validatorAddedEvent, false, nil
}
