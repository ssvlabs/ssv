package networkconfig

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	ethcommon "github.com/ethereum/go-ethereum/common"
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
	DomainType           string   `json:"DomainType,omitempty" yaml:"DomainType,omitempty"`
	RegistrySyncOffset   *big.Int `json:"RegistrySyncOffset,omitempty" yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr string   `json:"RegistryContractAddr,omitempty" yaml:"RegistryContractAddr,omitempty"`
	Bootnodes            []string `json:"Bootnodes,omitempty" yaml:"Bootnodes,omitempty"`
	DiscoveryProtocolID  string   `json:"DiscoveryProtocolID,omitempty" yaml:"DiscoveryProtocolID,omitempty"`
}

func (s *SSVConfig) marshal() (marshaledConfig, error) {
	aux := marshaledConfig{
		DomainType:           "0x" + hex.EncodeToString(s.DomainType[:]),
		RegistrySyncOffset:   s.RegistrySyncOffset,
		RegistryContractAddr: s.RegistryContractAddr.String(),
		Bootnodes:            s.Bootnodes,
		DiscoveryProtocolID:  "0x" + hex.EncodeToString(s.DiscoveryProtocolID[:]),
	}

	return aux, nil
}

func (s SSVConfig) MarshalJSON() ([]byte, error) {
	aux, err := s.marshal()
	if err != nil {
		return nil, err
	}

	return json.Marshal(aux)
}

func (s SSVConfig) MarshalYAML() (interface{}, error) {
	aux, err := s.marshal()
	if err != nil {
		return nil, err
	}

	return aux, nil
}

func (s *SSVConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	aux := &struct {
		DomainType           string   `yaml:"DomainType,omitempty"`
		RegistrySyncOffset   *big.Int `yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr string   `yaml:"RegistryContractAddr,omitempty"`
		Bootnodes            []string `yaml:"Bootnodes,omitempty"`
		DiscoveryProtocolID  string   `yaml:"DiscoveryProtocolID,omitempty"`
	}{}

	if err := unmarshal(aux); err != nil {
		return err
	}

	domain, err := hex.DecodeString(strings.TrimPrefix(aux.DomainType, "0x"))
	if err != nil {
		return fmt.Errorf("decode domain: %w", err)
	}

	var domainArr spectypes.DomainType
	if len(domain) != 0 {
		domainArr = spectypes.DomainType(domain)
	}

	discoveryProtocolID, err := hex.DecodeString(strings.TrimPrefix(aux.DiscoveryProtocolID, "0x"))
	if err != nil {
		return fmt.Errorf("decode discovery protocol ID: %w", err)
	}

	var discoveryProtocolIDArr [6]byte
	if len(discoveryProtocolID) != 0 {
		discoveryProtocolIDArr = [6]byte(discoveryProtocolID)
	}

	*s = SSVConfig{
		DomainType:           domainArr,
		RegistrySyncOffset:   aux.RegistrySyncOffset,
		RegistryContractAddr: ethcommon.HexToAddress(aux.RegistryContractAddr),
		Bootnodes:            aux.Bootnodes,
		DiscoveryProtocolID:  discoveryProtocolIDArr,
	}

	return nil
}

func (s SSVConfig) GetDomainType() spectypes.DomainType {
	return s.DomainType
}
