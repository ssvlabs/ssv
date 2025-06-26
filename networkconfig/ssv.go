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

var supportedSSVConfigs = map[string]SSVConfig{
	MainnetName:      MainnetSSV,
	HoleskyName:      HoleskySSV,
	HoleskyStageName: HoleskyStageSSV,
	LocalTestnetName: LocalTestnetSSV,
	HoleskyE2EName:   HoleskyE2ESSV,
	HoodiName:        HoodiSSV,
	HoodiStageName:   HoodiStageSSV,
	SepoliaName:      SepoliaSSV,
}

func GetSSVConfigByName(name string) (SSVConfig, error) {
	if network, ok := supportedSSVConfigs[name]; ok {
		return network, nil
	}

	return SSVConfig{}, fmt.Errorf("network not supported: %v", name)
}

type SSV interface {
	GetDomainType() spectypes.DomainType
	GetForks() SSVForkConfig
}

type SSVConfig struct {
	DomainType           spectypes.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
	// TotalEthereumValidators value needs to be maintained — consider getting it from external API
	// with default or per-network value(s) as fallback
	TotalEthereumValidators int

	Forks SSVForkConfig
}

func (s SSVConfig) String() string {
	marshaled, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}

	return string(marshaled)
}

type marshaledConfig struct {
	DomainType              hexutil.Bytes     `json:"DomainType,omitempty" yaml:"DomainType,omitempty"`
	RegistrySyncOffset      *big.Int          `json:"RegistrySyncOffset,omitempty" yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr    ethcommon.Address `json:"RegistryContractAddr,omitempty" yaml:"RegistryContractAddr,omitempty"`
	Bootnodes               []string          `json:"Bootnodes,omitempty" yaml:"Bootnodes,omitempty"`
	DiscoveryProtocolID     hexutil.Bytes     `json:"DiscoveryProtocolID,omitempty" yaml:"DiscoveryProtocolID,omitempty"`
	TotalEthereumValidators int               `json:"TotalEthereumValidators,omitempty" yaml:"TotalEthereumValidators,omitempty"`
	Forks                   SSVForkConfig     `json:"Forks,omitempty" yaml:"Forks,omitempty"`
}

// Helper method to avoid duplication between MarshalJSON and MarshalYAML
func (s SSVConfig) marshal() marshaledConfig {
	aux := marshaledConfig{
		DomainType:              s.DomainType[:],
		RegistrySyncOffset:      s.RegistrySyncOffset,
		RegistryContractAddr:    s.RegistryContractAddr,
		Bootnodes:               s.Bootnodes,
		DiscoveryProtocolID:     s.DiscoveryProtocolID[:],
		TotalEthereumValidators: s.TotalEthereumValidators,
		Forks:                   s.Forks,
	}

	return aux
}

func (s SSVConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.marshal())
}

func (s SSVConfig) MarshalYAML() (interface{}, error) {
	return s.marshal(), nil
}

// Helper method to avoid duplication between UnmarshalJSON and UnmarshalYAML
func (s *SSVConfig) unmarshalFromConfig(aux marshaledConfig) error {
	if len(aux.DomainType) != 4 {
		return fmt.Errorf("invalid domain type length: expected 4 bytes, got %d", len(aux.DomainType))
	}

	if len(aux.DiscoveryProtocolID) != 6 {
		return fmt.Errorf("invalid discovery protocol ID length: expected 6 bytes, got %d", len(aux.DiscoveryProtocolID))
	}

	*s = SSVConfig{
		DomainType:              spectypes.DomainType(aux.DomainType),
		RegistrySyncOffset:      aux.RegistrySyncOffset,
		RegistryContractAddr:    aux.RegistryContractAddr,
		Bootnodes:               aux.Bootnodes,
		DiscoveryProtocolID:     [6]byte(aux.DiscoveryProtocolID),
		TotalEthereumValidators: aux.TotalEthereumValidators,
		Forks:                   aux.Forks,
	}

	return nil
}

func (s *SSVConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var aux marshaledConfig
	if err := unmarshal(&aux); err != nil {
		return err
	}

	return s.unmarshalFromConfig(aux)
}

func (s *SSVConfig) UnmarshalJSON(data []byte) error {
	var aux marshaledConfig
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	return s.unmarshalFromConfig(aux)
}

func (s SSVConfig) GetDomainType() spectypes.DomainType {
	return s.DomainType
}

func (s SSVConfig) GetForks() SSVForkConfig {
	return s.Forks
}
