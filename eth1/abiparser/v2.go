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
	OperatorAdded            = "OperatorAdded"
	OperatorRemoved          = "OperatorRemoved"
	ValidatorAdded           = "ValidatorAdded"
	ValidatorRemoved         = "ValidatorRemoved"
	PodLiquidated            = "PodLiquidated"
	PodEnabled               = "PodEnabled"
	FeeRecipientAddressAdded = "FeeRecipientAddressAdded"
)

// OperatorAddedEvent struct represents event received by the smart contract
type OperatorAddedEvent struct {
	Id        uint64         //nolint
	Owner     common.Address // indexed
	PublicKey []byte
	Fee       *big.Int
}

// OperatorRemovedEvent struct represents event received by the smart contract
type OperatorRemovedEvent struct {
	Id uint64 //nolint
}

// ValidatorAddedEvent struct represents event received by the smart contract
type ValidatorAddedEvent struct {
	PublicKey          []byte
	OwnerAddress       common.Address
	OperatorPublicKeys [][]byte
	OperatorIds        []uint64
	SharePublicKeys    [][]byte
	EncryptedKeys      [][]byte
	Pod                Pod
}

// ValidatorRemovedEvent struct represents event received by the smart contract
type ValidatorRemovedEvent struct {
	OwnerAddress common.Address
	OperatorIds  []uint64
	PublicKey    []byte
	Pod          Pod
}

// PodLiquidatedEvent struct represents event received by the smart contract
type PodLiquidatedEvent struct {
	OwnerAddress common.Address
	OperatorIds  []uint64
	Pod          Pod
}

// PodEnabledEvent struct represents event received by the smart contract
type PodEnabledEvent struct {
	OwnerAddress common.Address
	OperatorIds  []uint64
	Pod          Pod
}

// FeeRecipientAddressAddedEvent struct represents event received by the smart contract
type FeeRecipientAddressAddedEvent struct {
	OwnerAddress     common.Address // indexed
	RecipientAddress common.Address
}

type Pod struct {
	ValidatorCount  uint32
	NetworkFee      uint64
	NetworkFeeIndex uint64
	Index           uint64
	Balance         uint64
	Disabled        bool
}

// AbiV2 parsing events from v2 abi contract
type AbiV2 struct {
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (v2 *AbiV2) ParseOperatorAddedEvent(
	log types.Log,
	contractAbi abi.ABI,
) (*OperatorAddedEvent, error) {
	var operatorAddedEvent OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&operatorAddedEvent, OperatorAdded, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
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

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", OperatorAdded),
		}
	}

	operatorAddedEvent.Owner = common.HexToAddress(log.Topics[1].Hex())
	return &operatorAddedEvent, nil
}

// ParseOperatorRemovedEvent parses OperatorRemovedEvent
func (v2 *AbiV2) ParseOperatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*OperatorRemovedEvent, error) {
	var operatorRemovedEvent OperatorRemovedEvent
	err := contractAbi.UnpackIntoInterface(&operatorRemovedEvent, OperatorRemoved, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	return &operatorRemovedEvent, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (v2 *AbiV2) ParseValidatorAddedEvent(
	log types.Log,
	contractAbi abi.ABI,
) (event *ValidatorAddedEvent, error error) {
	var validatorAddedEvent ValidatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&validatorAddedEvent, ValidatorAdded, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "could not define ABI")
	}

	for i, ek := range validatorAddedEvent.EncryptedKeys {
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
		validatorAddedEvent.EncryptedKeys[i] = []byte(encryptedSharePrivateKey)
	}

	return &validatorAddedEvent, nil
}

// ParseValidatorRemovedEvent parses ValidatorRemovedEvent
func (v2 *AbiV2) ParseValidatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*ValidatorRemovedEvent, error) {
	var validatorRemovedEvent ValidatorRemovedEvent
	err := contractAbi.UnpackIntoInterface(&validatorRemovedEvent, ValidatorRemoved, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	return &validatorRemovedEvent, nil
}

// ParsePodLiquidatedEvent parses PodLiquidatedEvent
func (v2 *AbiV2) ParsePodLiquidatedEvent(log types.Log, contractAbi abi.ABI) (*PodLiquidatedEvent, error) {
	var podLiquidatedEvent PodLiquidatedEvent
	err := contractAbi.UnpackIntoInterface(&podLiquidatedEvent, PodLiquidated, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	return &podLiquidatedEvent, nil
}

// ParsePodEnabledEvent parses PodEnabledEvent
func (v2 *AbiV2) ParsePodEnabledEvent(log types.Log, contractAbi abi.ABI) (*PodEnabledEvent, error) {
	var podEnabledEvent PodEnabledEvent
	err := contractAbi.UnpackIntoInterface(&podEnabledEvent, PodEnabled, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	return &podEnabledEvent, nil
}

// ParseFeeRecipientAddressAddedEvent parses FeeRecipientAddressAddedEvent
func (v2 *AbiV2) ParseFeeRecipientAddressAddedEvent(log types.Log, contractAbi abi.ABI) (*FeeRecipientAddressAddedEvent, error) {
	var feeRecipientAddressAddedEvent FeeRecipientAddressAddedEvent
	err := contractAbi.UnpackIntoInterface(&feeRecipientAddressAddedEvent, FeeRecipientAddressAdded, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", FeeRecipientAddressAdded),
		}
	}

	feeRecipientAddressAddedEvent.OwnerAddress = common.HexToAddress(log.Topics[1].Hex())
	return &feeRecipientAddressAddedEvent, nil
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
