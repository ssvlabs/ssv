package eventparser

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/eth/contract"
)

type EventParser struct {
	eventFilterer
	eventByIDGetter
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
}

type eventByIDGetter interface {
	EventByID(topic common.Hash) (*ethabi.Event, error)
}

func New(eventFilterer eventFilterer, contractABI eventByIDGetter) *EventParser {
	return &EventParser{
		eventFilterer:   eventFilterer,
		eventByIDGetter: contractABI,
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
	unpackedPubKey, err := unpackOperatorPublicKey(event.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("unpack OperatorAdded: %w", err)
	}

	// Contract stores a base64-encoded operator public key instead of plain one
	decodedPubKey, err := base64.StdEncoding.DecodeString(string(unpackedPubKey))
	if err != nil {
		return nil, fmt.Errorf("decode OperatorAdded: %w", err)
	}

	event.PublicKey = decodedPubKey

	return event, nil
}

func unpackOperatorPublicKey(fieldBytes []byte) ([]byte, error) {
	def := `[{ "name" : "method", "type": "function", "outputs": [{"type": "bytes"}]}]`
	// TODO: abigen?
	outAbi, err := abi.JSON(strings.NewReader(def))
	if err != nil {
		return nil, fmt.Errorf("define ABI: %w", err)
	}

	outField, err := outAbi.Unpack("method", fieldBytes)
	if err != nil {
		return nil, fmt.Errorf("unpack: %w", err)
	}

	unpacked, ok := outField[0].([]byte)
	if !ok {
		return nil, fmt.Errorf("cast OperatorPublicKey to []byte: %w", err)
	}

	return unpacked, nil
}
