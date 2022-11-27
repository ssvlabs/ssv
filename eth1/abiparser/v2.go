package abiparser

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
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
	OperatorRegistration  = "OperatorRegistration"
	OperatorRemoval       = "OperatorRemoval"
	ValidatorRegistration = "ValidatorRegistration"
	ValidatorRemoval      = "ValidatorRemoval"
	AccountLiquidation    = "AccountLiquidation"
	AccountEnable         = "AccountEnable"
)

// OperatorRegistrationEvent struct represents event received by the smart contract
type OperatorRegistrationEvent struct {
	Id           uint32 //nolint // indexed
	Name         string
	OwnerAddress common.Address // indexed
	PublicKey    []byte
	Fee          *big.Int
}

// OperatorRemovalEvent struct represents event received by the smart contract
type OperatorRemovalEvent struct {
	OperatorId   uint32         //nolint
	OwnerAddress common.Address // indexed
}

// ValidatorRegistrationEvent struct represents event received by the smart contract
type ValidatorRegistrationEvent struct {
	PublicKey          []byte
	OwnerAddress       common.Address // indexed
	OperatorPublicKeys [][]byte
	OperatorIds        []uint32
	SharesPublicKeys   [][]byte
	EncryptedKeys      [][]byte
}

// ValidatorRemovalEvent struct represents event received by the smart contract
type ValidatorRemovalEvent struct {
	OwnerAddress common.Address // indexed
	PublicKey    []byte
}

// AccountLiquidationEvent struct represents event received by the smart contract
type AccountLiquidationEvent struct {
	OwnerAddress common.Address // indexed
}

// AccountEnableEvent struct represents event received by the smart contract
type AccountEnableEvent struct {
	OwnerAddress common.Address // indexed
}

// AbiV2 parsing events from v2 abi contract
type AbiV2 struct {
}

// ParseOperatorRegistrationEvent parses an OperatorRegistrationEvent
func (v2 *AbiV2) ParseOperatorRegistrationEvent(
	log types.Log,
	contractAbi abi.ABI,
) (*OperatorRegistrationEvent, error) {
	var operatorRegistrationEvent OperatorRegistrationEvent
	err := contractAbi.UnpackIntoInterface(&operatorRegistrationEvent, OperatorRegistration, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}
	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "could not define ABI")
	}
	pubKey, err := readOperatorPubKey(operatorRegistrationEvent.PublicKey, outAbi)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read %s event operator public key", OperatorRegistration)
	}
	operatorRegistrationEvent.PublicKey = []byte(pubKey)

	if len(log.Topics) < 3 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", OperatorRegistration),
		}
	}
	operatorRegistrationEvent.Id = uint32(log.Topics[1].Big().Uint64())
	operatorRegistrationEvent.OwnerAddress = common.HexToAddress(log.Topics[2].Hex())
	return &operatorRegistrationEvent, nil
}

// ParseOperatorRemovalEvent parses OperatorRemovalEvent
func (v2 *AbiV2) ParseOperatorRemovalEvent(log types.Log, contractAbi abi.ABI) (*OperatorRemovalEvent, error) {
	var operatorRemovalEvent OperatorRemovalEvent
	err := contractAbi.UnpackIntoInterface(&operatorRemovalEvent, OperatorRemoval, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", OperatorRemoval),
		}
	}
	operatorRemovalEvent.OwnerAddress = common.HexToAddress(log.Topics[1].Hex())
	return &operatorRemovalEvent, nil
}

// ParseValidatorRegistrationEvent parses ValidatorRegistrationEvent
func (v2 *AbiV2) ParseValidatorRegistrationEvent(
	log types.Log,
	contractAbi abi.ABI,
) (event *ValidatorRegistrationEvent, error error) {
	var validatorRegistrationEvent ValidatorRegistrationEvent
	err := contractAbi.UnpackIntoInterface(&validatorRegistrationEvent, ValidatorRegistration, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "could not define ABI")
	}

	for i, ek := range validatorRegistrationEvent.EncryptedKeys {
		out, err := outAbi.Unpack("method", ek)
		if err != nil {
			return nil, &MalformedEventError{
				Err: errors.Wrap(err, "could not unpack EncryptedKey"),
			}
		}

		encryptedSharePrivateKey, ok := out[0].(string)
		if !ok {
			return nil, &MalformedEventError{
				Err: errors.Wrap(err, "could not cast EncryptedKey"),
			}
		}
		validatorRegistrationEvent.EncryptedKeys[i] = []byte(encryptedSharePrivateKey)
	}
	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ValidatorRegistration),
		}
	}
	validatorRegistrationEvent.OwnerAddress = common.HexToAddress(log.Topics[1].Hex())

	return &validatorRegistrationEvent, nil
}

// ParseValidatorRemovalEvent parses ValidatorRemovalEvent
func (v2 *AbiV2) ParseValidatorRemovalEvent(log types.Log, contractAbi abi.ABI) (*ValidatorRemovalEvent, error) {
	var validatorRemovalEvent ValidatorRemovalEvent
	err := contractAbi.UnpackIntoInterface(&validatorRemovalEvent, ValidatorRemoval, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ValidatorRemoval),
		}
	}
	validatorRemovalEvent.OwnerAddress = common.HexToAddress(log.Topics[1].Hex())

	return &validatorRemovalEvent, nil
}

// ParseAccountLiquidationEvent parses AccountLiquidationEvent
func (v2 *AbiV2) ParseAccountLiquidationEvent(log types.Log) (*AccountLiquidationEvent, error) {
	var accountLiquidationEvent AccountLiquidationEvent

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", AccountLiquidation),
		}
	}
	accountLiquidationEvent.OwnerAddress = common.HexToAddress(log.Topics[1].Hex())
	return &accountLiquidationEvent, nil
}

// ParseAccountEnableEvent parses AccountEnableEvent
func (v2 *AbiV2) ParseAccountEnableEvent(log types.Log) (*AccountEnableEvent, error) {
	var accountEnableEvent AccountEnableEvent

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", AccountEnable),
		}
	}
	accountEnableEvent.OwnerAddress = common.HexToAddress(log.Topics[1].Hex())
	return &accountEnableEvent, nil
}

func readOperatorPubKey(operatorPublicKey []byte, outAbi abi.ABI) (string, error) {
	outOperatorPublicKey, err := outAbi.Unpack("method", operatorPublicKey)
	if err != nil {
		return "", &MalformedEventError{
			Err: err,
		}
	}

	operatorPublicKeyString, ok := outOperatorPublicKey[0].(string)
	if !ok {
		return "", &MalformedEventError{
			Err: errors.Wrap(err, "could not cast OperatorPublicKey"),
		}
	}

	return operatorPublicKeyString, nil
}

func getOutAbi() (abi.ABI, error) {
	def := `[{ "name" : "method", "type": "function", "outputs": [{"type": "string"}]}]`
	return abi.JSON(strings.NewReader(def))
}
