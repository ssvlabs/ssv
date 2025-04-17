// Package eventparser implements functions for parsing registry contract events from
// Ethereum logs into their respective structured types.
package eventparser

import (
	"fmt"

	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/ssvlabs/ssv/eth/contract"
)

type EventParser struct {
	eventFilterer
	eventByIDGetter
	operatorPublicKeyABI *ethabi.ABI
}

type Parser interface {
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
func PackOperatorPublicKey(pubKey string) ([]byte, error) {
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

	outField, err := args.Pack([]byte(pubKey))
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}

	return outField, nil
}
