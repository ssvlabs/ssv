package eth1

import (
	"encoding/hex"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	"github.com/bloxapp/ssv/eth1/abiparser"
)

//go:generate mockgen -package=eth1 -destination=./mock_client.go -source=./client.go

type eventData interface {
	toEventData() interface{}
}

type operatorRegistrationEventYAML struct {
	Id           uint32 `yaml:"Id"`
	Name         string `yaml:"Name"`
	OwnerAddress string `yaml:"OwnerAddress"`
	PublicKey    string `yaml:"PublicKey"`
}

type operatorRemovalEventYAML struct {
	OperatorId   uint32 `yaml:"Id"`
	OwnerAddress string `yaml:"OwnerAddress"`
}

type validatorRegistrationEventYAML struct {
	PublicKey        string   `yaml:"PublicKey"`
	OwnerAddress     string   `yaml:"OwnerAddress"`
	OperatorIds      []uint32 `yaml:"OperatorIds"`
	SharesPublicKeys []string `yaml:"SharesPublicKeys"`
	EncryptedKeys    []string `yaml:"EncryptedKeys"`
}

type validatorRemovalEventYAML struct {
	OwnerAddress string `yaml:"OwnerAddress"`
	PublicKey    string `yaml:"PublicKey"`
}

type accountLiquidationEventYAML struct {
	OwnerAddress string `yaml:"OwnerAddress"`
}

type accountEnableEventYAML struct {
	OwnerAddress string `yaml:"OwnerAddress"`
}

func (e *operatorRegistrationEventYAML) toEventData() interface{} {
	return abiparser.OperatorRegistrationEvent{
		Id:           e.Id,
		Name:         e.Name,
		OwnerAddress: common.HexToAddress(e.OwnerAddress),
		PublicKey:    []byte(e.PublicKey),
	}
}

func (e *operatorRemovalEventYAML) toEventData() interface{} {
	return abiparser.OperatorRemovalEvent{
		OperatorId:   e.OperatorId,
		OwnerAddress: common.HexToAddress(e.OwnerAddress),
	}
}

func (e *validatorRegistrationEventYAML) toEventData() interface{} {
	var toByteArr = func(orig []string) [][]byte {
		res := make([][]byte, len(orig))
		for i, v := range orig {
			decodeString, err := hex.DecodeString(v)
			if err != nil {
				return nil
			}
			res[i] = decodeString
		}
		return res
	}
	var toByteArr2 = func(orig []string) [][]byte {
		res := make([][]byte, len(orig))
		for i, v := range orig {
			res[i] = []byte(v)
		}
		return res
	}
	decodeString, err := hex.DecodeString(e.PublicKey)
	if err != nil {
		return nil
	}
	return abiparser.ValidatorRegistrationEvent{
		PublicKey:        decodeString,
		OwnerAddress:     common.HexToAddress(e.OwnerAddress),
		OperatorIds:      e.OperatorIds,
		SharesPublicKeys: toByteArr(e.SharesPublicKeys),
		EncryptedKeys:    toByteArr2(e.EncryptedKeys),
	}
}

func (e *validatorRemovalEventYAML) toEventData() interface{} {
	return abiparser.ValidatorRemovalEvent{
		OwnerAddress: common.HexToAddress(e.OwnerAddress),
		PublicKey:    []byte(e.PublicKey),
	}
}

func (e *accountLiquidationEventYAML) toEventData() interface{} {
	return abiparser.AccountLiquidationEvent{
		OwnerAddress: common.HexToAddress(e.OwnerAddress),
	}
}

func (e *accountEnableEventYAML) toEventData() interface{} {
	return abiparser.AccountEnableEvent{
		OwnerAddress: common.HexToAddress(e.OwnerAddress),
	}
}

type eventDataUnmarshaler struct {
	name string
	data eventData
}

func (u *eventDataUnmarshaler) UnmarshalYAML(value *yaml.Node) error {
	var err error
	switch u.name {
	case "OperatorRegistration":
		var v operatorRegistrationEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "OperatorRemoval":
		var v operatorRemovalEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "ValidatorRegistration":
		var v validatorRegistrationEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "ValidatorRemoval":
		var v validatorRemovalEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "AccountLiquidation":
		var v accountLiquidationEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "AccountEnable":
		var v accountEnableEventYAML
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
	var ev struct {
		Data eventDataUnmarshaler `yaml:"Data"`
	}
	ev.Data.name = evName.Name

	if err := value.Decode(&ev); err != nil {
		return err
	}
	e.Name = ev.Data.name
	e.Data = ev.Data.data.toEventData()

	return nil
}
