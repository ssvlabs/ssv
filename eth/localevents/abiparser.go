package localevents

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
)

// Event names
const (
	OperatorAdded              = "OperatorAdded"
	OperatorRemoved            = "OperatorRemoved"
	ValidatorAdded             = "ValidatorAdded"
	ValidatorRemoved           = "ValidatorRemoved"
	ClusterLiquidated          = "ClusterLiquidated"
	ClusterReactivated         = "ClusterReactivated"
	FeeRecipientAddressUpdated = "FeeRecipientAddressUpdated"
)

// MalformedEventError is returned when event is malformed
type MalformedEventError struct {
	Err error
}

func (e *MalformedEventError) Error() string {
	return e.Err.Error()
}

// OperatorAddedEvent struct represents event received by the smart contract
type OperatorAddedEvent struct {
	OperatorId uint64         // indexed
	Owner      common.Address // indexed
	PublicKey  []byte
	Fee        *big.Int
}

// OperatorRemovedEvent struct represents event received by the smart contract
type OperatorRemovedEvent struct {
	OperatorId uint64 // indexed
}

// ValidatorAddedEvent struct represents event received by the smart contract
type ValidatorAddedEvent struct {
	Owner           common.Address // indexed
	OperatorIds     []uint64
	PublicKey       []byte
	Signature       []byte
	TxHash          common.Hash
	Shares          []byte
	SharePublicKeys [][]byte
	EncryptedKeys   [][]byte
	Cluster         Cluster
}

// ValidatorRemovedEvent struct represents event received by the smart contract
type ValidatorRemovedEvent struct {
	Owner       common.Address // indexed
	OperatorIds []uint64
	PublicKey   []byte
	Cluster     Cluster
}

// ClusterLiquidatedEvent struct represents event received by the smart contract
type ClusterLiquidatedEvent struct {
	Owner       common.Address // indexed
	OperatorIds []uint64
	Cluster     Cluster
}

// ClusterReactivatedEvent struct represents event received by the smart contract
type ClusterReactivatedEvent struct {
	Owner       common.Address // indexed
	OperatorIds []uint64
	Cluster     Cluster
}

// FeeRecipientAddressUpdatedEvent struct represents event received by the smart contract
type FeeRecipientAddressUpdatedEvent struct {
	Owner            common.Address // indexed
	RecipientAddress common.Address
}

type Cluster struct {
	ValidatorCount  uint32
	NetworkFeeIndex uint64
	Index           uint64
	Active          bool
	Balance         *big.Int
}

// AbiV1 parsing events from v1 abi contract
type AbiV1 struct {
}

// ParseOperatorAddedEvent parses an OperatorAddedEvent
func (v1 *AbiV1) ParseOperatorAddedEvent(log types.Log, contractAbi abi.ABI) (*OperatorAddedEvent, error) {
	var event OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&event, OperatorAdded, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}
	pubKey, err := unpackField(event.PublicKey)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read %s event operator public key", OperatorAdded)
	}
	event.PublicKey = pubKey

	if len(log.Topics) < 3 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", OperatorAdded),
		}
	}
	event.OperatorId = log.Topics[1].Big().Uint64()
	event.Owner = common.HexToAddress(log.Topics[2].Hex())

	return &event, nil
}

// ParseOperatorRemovedEvent parses OperatorRemovedEvent
func (v1 *AbiV1) ParseOperatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*OperatorRemovedEvent, error) {
	var event OperatorRemovedEvent
	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", OperatorRemoved),
		}
	}
	event.OperatorId = log.Topics[1].Big().Uint64()

	return &event, nil
}

// ParseValidatorAddedEvent parses ValidatorAddedEvent
func (v1 *AbiV1) ParseValidatorAddedEvent(log types.Log, contractAbi abi.ABI) (*ValidatorAddedEvent, error) {
	// NOTE: we are not incrementing the nonce on the below malformed events because they should never happen
	var event ValidatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&event, ValidatorAdded, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ValidatorAdded),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())
	event.TxHash = log.TxHash

	return &event, nil
}

// ParseValidatorRemovedEvent parses ValidatorRemovedEvent
func (v1 *AbiV1) ParseValidatorRemovedEvent(log types.Log, contractAbi abi.ABI) (*ValidatorRemovedEvent, error) {
	var event ValidatorRemovedEvent
	err := contractAbi.UnpackIntoInterface(&event, ValidatorRemoved, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ValidatorRemoved),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())

	return &event, nil
}

// ParseClusterLiquidatedEvent parses ClusterLiquidatedEvent
func (v1 *AbiV1) ParseClusterLiquidatedEvent(log types.Log, contractAbi abi.ABI) (*ClusterLiquidatedEvent, error) {
	var event ClusterLiquidatedEvent
	err := contractAbi.UnpackIntoInterface(&event, ClusterLiquidated, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ClusterLiquidated),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())

	return &event, nil
}

// ParseClusterReactivatedEvent parses ClusterReactivatedEvent
func (v1 *AbiV1) ParseClusterReactivatedEvent(log types.Log, contractAbi abi.ABI) (*ClusterReactivatedEvent, error) {
	var event ClusterReactivatedEvent
	err := contractAbi.UnpackIntoInterface(&event, ClusterReactivated, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", ClusterReactivated),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())

	return &event, nil
}

// ParseFeeRecipientAddressUpdatedEvent parses FeeRecipientAddressUpdatedEvent
func (v1 *AbiV1) ParseFeeRecipientAddressUpdatedEvent(log types.Log, contractAbi abi.ABI) (*FeeRecipientAddressUpdatedEvent, error) {
	var event FeeRecipientAddressUpdatedEvent
	err := contractAbi.UnpackIntoInterface(&event, FeeRecipientAddressUpdated, log.Data)
	if err != nil {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not unpack event"),
		}
	}

	if len(log.Topics) < 2 {
		return nil, &MalformedEventError{
			Err: errors.Errorf("%s event missing topics", FeeRecipientAddressUpdated),
		}
	}
	event.Owner = common.HexToAddress(log.Topics[1].Hex())

	return &event, nil
}

func unpackField(fieldBytes []byte) ([]byte, error) {
	outAbi, err := getOutAbi()
	if err != nil {
		return nil, errors.Wrap(err, "could not define ABI")
	}

	outField, err := outAbi.Unpack("method", fieldBytes)
	if err != nil {
		return nil, &MalformedEventError{
			Err: err,
		}
	}

	unpacked, ok := outField[0].([]byte)
	if !ok {
		return nil, &MalformedEventError{
			Err: errors.Wrap(err, "could not cast OperatorPublicKey"),
		}
	}

	return unpacked, nil
}

func getOutAbi() (abi.ABI, error) {
	def := `[{ "name" : "method", "type": "function", "outputs": [{"type": "bytes"}]}]`
	return abi.JSON(strings.NewReader(def))
}
