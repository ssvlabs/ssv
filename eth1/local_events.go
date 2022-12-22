package eth1

import (
	"encoding/hex"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	"github.com/bloxapp/ssv/eth1/abiparser"
)

//go:generate mockgen -package=eth1 -destination=./mock_client.go -source=./client.go

type eventData interface {
	toEventData() (interface{}, error)
}

type operatorAddedEventYAML struct {
	Id        uint64 `yaml:"Id"`
	Owner     string `yaml:"Owner"`
	PublicKey string `yaml:"PublicKey"`
}

type OperatorRemovedEventYAML struct {
	OperatorId uint64 `yaml:"Id"`
}

type validatorAddedEventYAML struct {
	PublicKey       string   `yaml:"PublicKey"`
	OwnerAddress    string   `yaml:"OwnerAddress"`
	OperatorIds     []uint64 `yaml:"OperatorIds"`
	SharePublicKeys []string `yaml:"SharePublicKeys"`
	EncryptedKeys   []string `yaml:"EncryptedKeys"`
}

type ValidatorRemovedEventYAML struct {
	OwnerAddress string   `yaml:"OwnerAddress"`
	OperatorIds  []uint64 `yaml:"OperatorIds"`
	PublicKey    string   `yaml:"PublicKey"`
}

type PodLiquidatedEventYAML struct {
	OwnerAddress string   `yaml:"OwnerAddress"`
	OperatorIds  []uint64 `yaml:"OperatorIds"`
}

type PodEnabledEventYAML struct {
	OwnerAddress string   `yaml:"OwnerAddress"`
	OperatorIds  []uint64 `yaml:"OperatorIds"`
}

func (e *operatorAddedEventYAML) toEventData() (interface{}, error) {
	return abiparser.OperatorAddedEvent{
		Id:        e.Id,
		Owner:     common.HexToAddress(e.Owner),
		PublicKey: []byte(e.PublicKey),
	}, nil
}

func (e *OperatorRemovedEventYAML) toEventData() (interface{}, error) {
	return abiparser.OperatorRemovedEvent{
		Id: e.OperatorId,
	}, nil
}

func (e *validatorAddedEventYAML) toEventData() (interface{}, error) {
	pubKey, err := hex.DecodeString(strings.TrimPrefix(e.PublicKey, "0x"))
	if err != nil {
		return nil, err
	}
	sharePubKeys, err := toByteArr(e.SharePublicKeys, true)
	if err != nil {
		return nil, err
	}
	encryptedKeys, err := toByteArr(e.EncryptedKeys, false)
	if err != nil {
		return nil, err
	}
	return abiparser.ValidatorAddedEvent{
		PublicKey:       pubKey,
		OwnerAddress:    common.HexToAddress(e.OwnerAddress),
		OperatorIds:     e.OperatorIds,
		SharePublicKeys: sharePubKeys,
		EncryptedKeys:   encryptedKeys,
	}, nil
}

func (e *ValidatorRemovedEventYAML) toEventData() (interface{}, error) {
	return abiparser.ValidatorRemovedEvent{
		OwnerAddress: common.HexToAddress(e.OwnerAddress),
		OperatorIds:  e.OperatorIds,
		PublicKey:    []byte(strings.TrimPrefix(e.PublicKey, "0x")),
	}, nil
}

func (e *PodLiquidatedEventYAML) toEventData() (interface{}, error) {
	return abiparser.PodLiquidatedEvent{
		OwnerAddress: common.HexToAddress(e.OwnerAddress),
		OperatorIds:  e.OperatorIds,
	}, nil
}

func (e *PodEnabledEventYAML) toEventData() (interface{}, error) {
	return abiparser.PodEnabledEvent{
		OwnerAddress: common.HexToAddress(e.OwnerAddress),
		OperatorIds:  e.OperatorIds,
	}, nil
}

type eventDataUnmarshaler struct {
	name string
	data eventData
}

func (u *eventDataUnmarshaler) UnmarshalYAML(value *yaml.Node) error {
	var err error
	switch u.name {
	case "OperatorAdded":
		var v operatorAddedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "OperatorRemoved":
		var v OperatorRemovedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "ValidatorAdded":
		var v validatorAddedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "ValidatorRemoved":
		var v ValidatorRemovedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "PodLiquidated":
		var v PodLiquidatedEventYAML
		err = value.Decode(&v)
		u.data = &v
	case "PodEnabled":
		var v PodEnabledEventYAML
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

func toByteArr(orig []string, decodeHex bool) ([][]byte, error) {
	res := make([][]byte, len(orig))
	for i, v := range orig {
		if decodeHex {
			d, err := hex.DecodeString(strings.TrimPrefix(v, "0x"))
			if err != nil {
				return nil, err
			}
			res[i] = d
		} else {
			res[i] = []byte(v)
		}
	}
	return res, nil
}
