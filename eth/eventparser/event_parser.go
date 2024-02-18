// Package eventparser implements functions for parsing registry contract events from
// Ethereum logs into their respective structured types.
package eventparser

import (
	"fmt"

	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/eth/contract"
)

// Event names.
const (
	OperatorAdded              = "OperatorAdded"
	OperatorRemoved            = "OperatorRemoved"
	ValidatorAdded             = "ValidatorAdded"
	ValidatorRemoved           = "ValidatorRemoved"
	ClusterLiquidated          = "ClusterLiquidated"
	ClusterReactivated         = "ClusterReactivated"
	FeeRecipientAddressUpdated = "FeeRecipientAddressUpdated"
	ValidatorExited            = "ValidatorExited"
)

var (
	ErrUnknownEvent = fmt.Errorf("unknown event")
)

type EventParser struct {
	eventFilterer
	eventByIDGetter
	operatorPublicKeyABI *ethabi.ABI
}

type Parser interface {
	ParseEvent(abiEvent *ethabi.Event, event ethtypes.Log) (interface{}, error)
	eventFilterer
	eventByIDGetter
}

type eventFilterer interface {
	ParseOperatorAdded(log ethtypes.Log) (*contract.ContractOperatorAdded, error)
	ParseOperatorRemoved(log ethtypes.Log) (*contract.ContractOperatorRemoved, error)
	ParseValidatorAdded(log ethtypes.Log) (*contract.ContractValidatorAdded, error)
	ParseValidatorRemoved(log ethtypes.Log) (*contract.ContractValidatorRemoved, error)
	ParseClusterLiquidated(log ethtypes.Log) (*contract.ContractClusterLiquidated, error)
	ParseClusterReactivated(log ethtypes.Log) (*contract.ContractClusterReactivated, error)
	ParseFeeRecipientAddressUpdated(log ethtypes.Log) (*contract.ContractFeeRecipientAddressUpdated, error)
	ParseValidatorExited(log ethtypes.Log) (*contract.ContractValidatorExited, error)
}

type eventByIDGetter interface {
	EventByID(topic ethcommon.Hash) (*ethabi.Event, error)
}

func New(eventFilterer eventFilterer) *EventParser {
	contractABI, err := contract.ContractMetaData.GetAbi()
	if err != nil {
		panic(err)
	}

	operatorPublicKeyABI, err := contract.OperatorPublicKeyMetaData.GetAbi()
	if err != nil {
		panic(err)
	}

	return &EventParser{
		eventFilterer:        eventFilterer,
		eventByIDGetter:      contractABI,
		operatorPublicKeyABI: operatorPublicKeyABI,
	}
}

func (e *EventParser) ParseEvent(abiEvent *ethabi.Event, event ethtypes.Log) (interface{}, error) {
	switch abiEvent.Name {
	case OperatorAdded:
		return e.ParseOperatorAdded(event)
	case OperatorRemoved:
		return e.ParseOperatorRemoved(event)
	case ValidatorAdded:
		return e.ParseValidatorAdded(event)
	case ValidatorRemoved:
		return e.ParseValidatorRemoved(event)
	case ClusterLiquidated:
		return e.ParseClusterLiquidated(event)
	case ClusterReactivated:
		return e.ParseClusterReactivated(event)
	case FeeRecipientAddressUpdated:
		return e.ParseFeeRecipientAddressUpdated(event)
	case ValidatorExited:
		return e.ParseValidatorExited(event)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownEvent, abiEvent.Name)
	}
}

func (e *EventParser) ParseOperatorAdded(log ethtypes.Log) (*contract.ContractOperatorAdded, error) {
	event, err := e.eventFilterer.ParseOperatorAdded(log)
	if err != nil {
		return nil, err
	}

	// Since event.PublicKey is not the operator public key itself
	// (https://github.com/bloxapp/automation-Tools/blob/6f25a4bd67b6d01e13e300f8585eeb34f37070eb/helpers/contract-integration/register-operators.ts#L33)
	// but packed operator public key, it needs to be unpacked.
	unp, err := e.unpackOperatorPublicKey(event.PublicKey)
	if err != nil {
		return nil, err
	}
	event.PublicKey = unp

	return event, nil
}

func (e *EventParser) unpackOperatorPublicKey(fieldBytes []byte) ([]byte, error) {
	outField, err := e.operatorPublicKeyABI.Unpack("method", fieldBytes)
	if err != nil {
		return nil, fmt.Errorf("unpack: %w", err)
	}

	unpacked, ok := outField[0].([]byte)
	if !ok {
		return nil, fmt.Errorf("cast OperatorPublicKey to []byte: %w", err)
	}

	return unpacked, nil
}

// PackOperatorPublicKey is used for testing only, packing the operator pubkey bytes into an event.
func PackOperatorPublicKey(fieldBytes []byte) ([]byte, error) {
	byts, err := ethabi.NewType("bytes", "bytes", nil)
	if err != nil {
		return nil, err
	}

	args := ethabi.Arguments{
		{
			Name: "publicKey",
			Type: byts,
		},
	}

	outField, err := args.Pack(fieldBytes)
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}

	return outField, nil
}
