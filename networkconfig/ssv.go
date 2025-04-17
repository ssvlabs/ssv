package networkconfig

import (
	"encoding/json"
	"fmt"
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./ssv_mock.go -source=./ssv.go

var SupportedSSVConfigs = map[string]SSVConfig{
	Mainnet.Name:      Mainnet.SSVConfig,
	Holesky.Name:      Holesky.SSVConfig,
	HoleskyStage.Name: HoleskyStage.SSVConfig,
	LocalTestnet.Name: LocalTestnet.SSVConfig,
	HoleskyE2E.Name:   HoleskyE2E.SSVConfig,
	Hoodi.Name:        Hoodi.SSVConfig,
	HoodiStage.Name:   HoodiStage.SSVConfig,
	Sepolia.Name:      Sepolia.SSVConfig,
}

func GetSSVConfigByName(name string) (SSVConfig, error) {
	if network, ok := SupportedSSVConfigs[name]; ok {
		return network, nil
	}

	return SSVConfig{}, fmt.Errorf("network not supported: %v", name)
}

type SSV interface {
	GetDomainType() spectypes.DomainType
}

type SSVConfig struct {
	DomainType           spectypes.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
}

func (s SSVConfig) String() string {
	marshaled, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}

	return string(marshaled)
}

type marshaledConfig struct {
	DomainType           hexutil.Bytes     `json:"DomainType,omitempty" yaml:"DomainType,omitempty"`
	RegistrySyncOffset   *big.Int          `json:"RegistrySyncOffset,omitempty" yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr ethcommon.Address `json:"RegistryContractAddr,omitempty" yaml:"RegistryContractAddr,omitempty"`
	Bootnodes            []string          `json:"Bootnodes,omitempty" yaml:"Bootnodes,omitempty"`
	DiscoveryProtocolID  hexutil.Bytes     `json:"DiscoveryProtocolID,omitempty" yaml:"DiscoveryProtocolID,omitempty"`
}

func (s SSVConfig) marshal() marshaledConfig {
	aux := marshaledConfig{
		DomainType:           s.DomainType[:],
		RegistrySyncOffset:   s.RegistrySyncOffset,
		RegistryContractAddr: s.RegistryContractAddr,
		Bootnodes:            s.Bootnodes,
		DiscoveryProtocolID:  s.DiscoveryProtocolID[:],
	}

	return aux
}

func (s SSVConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.marshal())
}

func (s SSVConfig) MarshalYAML() (interface{}, error) {
	return s.marshal(), nil
}

func (s *SSVConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	aux := &struct {
		DomainType           hexutil.Bytes     `yaml:"DomainType,omitempty"`
		RegistrySyncOffset   *big.Int          `yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr ethcommon.Address `yaml:"RegistryContractAddr,omitempty"`
		Bootnodes            []string          `yaml:"Bootnodes,omitempty"`
		DiscoveryProtocolID  hexutil.Bytes     `yaml:"DiscoveryProtocolID,omitempty"`
	}{}

	if err := unmarshal(aux); err != nil {
		return err
	}

	if len(aux.DomainType) != 4 {
		return fmt.Errorf("invalid domain type length: expected 4 bytes, got %d", len(aux.DomainType))
	}

	if len(aux.DiscoveryProtocolID) != 6 {
		return fmt.Errorf("invalid discovery protocol ID length: expected 6 bytes, got %d", len(aux.DiscoveryProtocolID))
	}

	*s = SSVConfig{
		DomainType:           spectypes.DomainType(aux.DomainType),
		RegistrySyncOffset:   aux.RegistrySyncOffset,
		RegistryContractAddr: aux.RegistryContractAddr,
		Bootnodes:            aux.Bootnodes,
		DiscoveryProtocolID:  [6]byte(aux.DiscoveryProtocolID),
	}

	return nil
}

func (s *SSVConfig) UnmarshalJSON(data []byte) error {
	aux := &struct {
		DomainType           hexutil.Bytes     `json:"DomainType,omitempty"`
		RegistrySyncOffset   *big.Int          `json:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr ethcommon.Address `json:"RegistryContractAddr,omitempty"`
		Bootnodes            []string          `json:"Bootnodes,omitempty"`
		DiscoveryProtocolID  hexutil.Bytes     `json:"DiscoveryProtocolID,omitempty"`
	}{}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if len(aux.DomainType) != 4 {
		return fmt.Errorf("invalid domain type length: expected 4 bytes, got %d", len(aux.DomainType))
	}

	if len(aux.DiscoveryProtocolID) != 6 {
		return fmt.Errorf("invalid discovery protocol ID length: expected 6 bytes, got %d", len(aux.DiscoveryProtocolID))
	}

	*s = SSVConfig{
		DomainType:           spectypes.DomainType(aux.DomainType),
		RegistrySyncOffset:   aux.RegistrySyncOffset,
		RegistryContractAddr: aux.RegistryContractAddr,
		Bootnodes:            aux.Bootnodes,
		DiscoveryProtocolID:  [6]byte(aux.DiscoveryProtocolID),
	}

	return nil
}

func (s SSVConfig) GetDomainType() spectypes.DomainType {
	return s.DomainType
}
