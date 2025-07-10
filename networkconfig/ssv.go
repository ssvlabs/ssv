package networkconfig

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var supportedSSVConfigs = map[string]*SSVConfig{
	MainnetName:      MainnetSSV,
	HoleskyName:      HoleskySSV,
	HoleskyStageName: HoleskyStageSSV,
	LocalTestnetName: LocalTestnetSSV,
	HoleskyE2EName:   HoleskyE2ESSV,
	HoodiName:        HoodiSSV,
	HoodiStageName:   HoodiStageSSV,
	SepoliaName:      SepoliaSSV,
}

func GetSSVConfigByName(name string) (*SSVConfig, error) {
	if network, ok := supportedSSVConfigs[name]; ok {
		return network, nil
	}

	return nil, fmt.Errorf("network not supported: %v", name)
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
	// GasLimit36Epoch is an epoch when to upgrade from default gas limit value of 30_000_000
	// to 36_000_000.
	GasLimit36Epoch phase0.Epoch
}

func (s *SSVConfig) String() string {
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
	GasLimit36Epoch         phase0.Epoch      `json:"GasLimit36Epoch,omitempty" yaml:"GasLimit36Epoch,omitempty"`
}

// Helper method to avoid duplication between MarshalJSON and MarshalYAML
func (s *SSVConfig) marshal() *marshaledConfig {
	return &marshaledConfig{
		DomainType:              s.DomainType[:],
		RegistrySyncOffset:      s.RegistrySyncOffset,
		RegistryContractAddr:    s.RegistryContractAddr,
		Bootnodes:               s.Bootnodes,
		DiscoveryProtocolID:     s.DiscoveryProtocolID[:],
		TotalEthereumValidators: s.TotalEthereumValidators,
		GasLimit36Epoch:         s.GasLimit36Epoch,
	}
}

func (s *SSVConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.marshal())
}

func (s *SSVConfig) MarshalYAML() (interface{}, error) {
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
		GasLimit36Epoch:         aux.GasLimit36Epoch,
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

func (s *SSVConfig) GetDomainType() spectypes.DomainType {
	return s.DomainType
}

func (s *SSVConfig) GetGasLimit36Epoch() phase0.Epoch {
	return s.GasLimit36Epoch
}
