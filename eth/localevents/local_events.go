package localevents

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"

	"github.com/bloxapp/ssv/eth/contract"
)

// Event represents an eth1 event log in the system
// TODO: It has no Log field because it's not used and seems unnecessary. However, we need to make sure we don't need it.
type Event struct {
	// Name is the event name used for internal representation.
	Name string
	// Data is the parsed event
	Data interface{}
}

func Load(path string) ([]Event, error) {
	yamlFile, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	var events []Event
	if err := yaml.Unmarshal(yamlFile, &events); err != nil {
		return nil, err
	}

	return events, nil
}

type eventData interface {
	toEventData() (interface{}, error)
}

type eventDataUnmarshaler struct {
	name string
	data eventData
}

type OperatorAddedEventYAML struct {
	ID        uint64 `yaml:"ID"`
	Owner     string `yaml:"Owner"`
	PublicKey string `yaml:"PublicKey"`
}

type OperatorRemovedEventYAML struct {
	ID uint64 `yaml:"ID"`
}

type ValidatorAddedEventYAML struct {
	PublicKey   string   `yaml:"PublicKey"`
	Owner       string   `yaml:"Owner"`
	OperatorIds []uint64 `yaml:"OperatorIds"`
	Shares      string   `yaml:"Shares"`
}

type ValidatorRemovedEventYAML struct {
	Owner       string   `yaml:"Owner"`
	OperatorIds []uint64 `yaml:"OperatorIds"`
	PublicKey   string   `yaml:"PublicKey"`
}

type ClusterLiquidatedEventYAML struct {
	Owner       string   `yaml:"Owner"`
	OperatorIds []uint64 `yaml:"OperatorIds"`
}

type ClusterReactivatedEventYAML struct {
	Owner       string   `yaml:"Owner"`
	OperatorIds []uint64 `yaml:"OperatorIds"`
}

type FeeRecipientAddressUpdatedEventYAML struct {
	Owner            string `yaml:"Owner"`
	RecipientAddress string `yaml:"RecipientAddress"`
}

type ValidatorExitedEventYAML struct {
	PublicKey   string   `yaml:"PublicKey"`
	OperatorIds []uint64 `yaml:"OperatorIds"`
}

func (e *OperatorAddedEventYAML) toEventData() (interface{}, error) {
	return contract.ContractOperatorAdded{
		OperatorId: e.ID,
		Owner:      ethcommon.HexToAddress(e.Owner),
		PublicKey:  []byte(e.PublicKey),
	}, nil
}

func (e *OperatorRemovedEventYAML) toEventData() (interface{}, error) {
	return contract.ContractOperatorRemoved{
		OperatorId: e.ID,
	}, nil
}

func (e *ValidatorAddedEventYAML) toEventData() (interface{}, error) {
	pubKey, err := hex.DecodeString(strings.TrimPrefix(e.PublicKey, "0x"))
	if err != nil {
		return nil, err
	}

	shares, err := hex.DecodeString(strings.TrimPrefix(e.Shares, "0x"))
	if err != nil {
		return nil, err
	}

	return contract.ContractValidatorAdded{
		PublicKey:   pubKey,
		Owner:       ethcommon.HexToAddress(e.Owner),
		OperatorIds: e.OperatorIds,
		Shares:      shares,
	}, nil
}

func (e *ValidatorRemovedEventYAML) toEventData() (interface{}, error) {
	return contract.ContractValidatorRemoved{
		Owner:       ethcommon.HexToAddress(e.Owner),
		OperatorIds: e.OperatorIds,
		PublicKey:   []byte(strings.TrimPrefix(e.PublicKey, "0x")),
	}, nil
}

func (e *ClusterLiquidatedEventYAML) toEventData() (interface{}, error) {
	return contract.ContractClusterLiquidated{
		Owner:       ethcommon.HexToAddress(e.Owner),
		OperatorIds: e.OperatorIds,
	}, nil
}

func (e *ClusterReactivatedEventYAML) toEventData() (interface{}, error) {
	return contract.ContractClusterReactivated{
		Owner:       ethcommon.HexToAddress(e.Owner),
		OperatorIds: e.OperatorIds,
	}, nil
}

func (e *FeeRecipientAddressUpdatedEventYAML) toEventData() (interface{}, error) {
	return contract.ContractFeeRecipientAddressUpdated{
		Owner:            ethcommon.HexToAddress(e.Owner),
		RecipientAddress: ethcommon.HexToAddress(e.RecipientAddress),
	}, nil
}

func (e *ValidatorExitedEventYAML) toEventData() (interface{}, error) {
	return contract.ContractValidatorExited{
		PublicKey:   []byte(strings.TrimPrefix(e.PublicKey, "0x")),
		OperatorIds: e.OperatorIds,
	}, nil
}

func (u *eventDataUnmarshaler) UnmarshalYAML(value *yaml.Node) error {
	var err error
	switch u.name {
	case "OperatorAdded":
		var v OperatorAddedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "OperatorRemoved":
		var v OperatorRemovedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "ValidatorAdded":
		var v ValidatorAddedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "ValidatorRemoved":
		var v ValidatorRemovedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "ClusterLiquidated":
		var v ClusterLiquidatedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "ClusterReactivated":
		var v ClusterReactivatedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "FeeRecipientAddressUpdated":
		var v FeeRecipientAddressUpdatedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "ValidatorExited":
		var v ValidatorExitedEventYAML
		err = value.Decode(&v)
		u.data = &v
	default:
		return errors.New("event unknown")
	}

	return err
}

func (e *Event) UnmarshalYAML(value *yaml.Node) error {
	var evName struct {
		Name string `yaml:"Name"`
	}
	err := value.Decode(&evName)
	if err != nil {
		return err
	}
	if evName.Name == "" {
		return errors.New("event name is empty")
	}
	var ev struct {
		Data eventDataUnmarshaler `yaml:"Data"`
	}
	ev.Data.name = evName.Name

	if err := value.Decode(&ev); err != nil {
		return err
	}
	if ev.Data.data == nil {
		return errors.New("event data is nil")
	}
	e.Name = ev.Data.name
	data, err := ev.Data.data.toEventData()
	if err != nil {
		return err
	}
	e.Data = data

	return nil
}
