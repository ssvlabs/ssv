package networkconfig

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/sanity-io/litter"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var SupportedSSVConfigs = map[string]SSV{
	MainnetSSV.Name:      MainnetSSV,
	HoleskySSV.Name:      HoleskySSV,
	HoleskyStageSSV.Name: HoleskyStageSSV,
	LocalTestnetSSV.Name: LocalTestnetSSV,
	HoleskyE2ESSV.Name:   HoleskyE2ESSV,
}

func GetSSVConfigByName(name string) (SSV, error) {
	if network, ok := SupportedSSVConfigs[name]; ok {
		return network, nil
	}

	return SSV{}, fmt.Errorf("network not supported: %v", name)
}

type SSV struct {
	Name                    string               `yaml:"Name,omitempty"`
	DomainType              spectypes.DomainType `yaml:"DomainType,omitempty"`
	RegistrySyncOffset      *big.Int             `yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr    ethcommon.Address    `yaml:"RegistryContractAddr,omitempty"`
	Bootnodes               []string             `yaml:"Bootnodes,omitempty"`
	DiscoveryProtocolID     [6]byte              `yaml:"DiscoveryProtocolID,omitempty"`
	TotalEthereumValidators int                  `yaml:"TotalEthereumValidators,omitempty"` // value needs to be maintained — consider getting it from external API with default or per-network value(s) as fallback
}

func (s SSV) String() string {
	return litter.Sdump(s)
}

func (s SSV) MarshalYAML() (interface{}, error) {
	aux := &struct {
		Name                    string   `yaml:"Name,omitempty"`
		DomainType              string   `yaml:"DomainType,omitempty"`
		RegistrySyncOffset      *big.Int `yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr    string   `yaml:"RegistryContractAddr,omitempty"`
		Bootnodes               []string `yaml:"Bootnodes,omitempty"`
		DiscoveryProtocolID     string   `yaml:"DiscoveryProtocolID,omitempty"`
		TotalEthereumValidators int      `yaml:"TotalEthereumValidators,omitempty"`
	}{
		Name:                    s.Name,
		DomainType:              "0x" + hex.EncodeToString(s.DomainType[:]),
		RegistrySyncOffset:      s.RegistrySyncOffset,
		RegistryContractAddr:    s.RegistryContractAddr.String(),
		Bootnodes:               s.Bootnodes,
		DiscoveryProtocolID:     "0x" + hex.EncodeToString(s.DiscoveryProtocolID[:]),
		TotalEthereumValidators: s.TotalEthereumValidators,
	}

	return aux, nil
}

func (s *SSV) UnmarshalYAML(unmarshal func(interface{}) error) error {
	aux := &struct {
		Name                    string   `yaml:"Name,omitempty"`
		DomainType              string   `yaml:"DomainType,omitempty"`
		RegistrySyncOffset      *big.Int `yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr    string   `yaml:"RegistryContractAddr,omitempty"`
		Bootnodes               []string `yaml:"Bootnodes,omitempty"`
		DiscoveryProtocolID     string   `yaml:"DiscoveryProtocolID,omitempty"`
		TotalEthereumValidators int      `yaml:"TotalEthereumValidators,omitempty"`
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

	*s = SSV{
		Name:                    aux.Name,
		DomainType:              domainArr,
		RegistrySyncOffset:      aux.RegistrySyncOffset,
		RegistryContractAddr:    ethcommon.HexToAddress(aux.RegistryContractAddr),
		Bootnodes:               aux.Bootnodes,
		DiscoveryProtocolID:     discoveryProtocolIDArr,
		TotalEthereumValidators: aux.TotalEthereumValidators,
	}

	return nil
}

const forkName = "alan"

func (s SSV) ConfigName() string {
	return fmt.Sprintf("%s:%s", s.Name, forkName)
}
