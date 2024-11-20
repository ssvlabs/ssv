package networkconfig

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const alanForkName = "alan"

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
	Name                      string               `yaml:"Name,omitempty"`
	GenesisDomainType         spectypes.DomainType `yaml:"GenesisDomainType,omitempty"`
	AlanDomainType            spectypes.DomainType `yaml:"AlanDomainType,omitempty"`
	RegistrySyncOffset        *big.Int             `yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr      ethcommon.Address    `yaml:"RegistryContractAddr,omitempty"`
	Bootnodes                 []string             `yaml:"Bootnodes,omitempty"`
	DiscoveryProtocolID       [6]byte              `yaml:"DiscoveryProtocolID,omitempty"`
	AlanForkEpoch             phase0.Epoch         `yaml:"AlanForkEpoch,omitempty"`
	MaxValidatorsPerCommittee int                  `yaml:"MaxValidatorsPerCommittee,omitempty"`
	TotalEthereumValidators   int                  `yaml:"TotalEthereumValidators,omitempty"` // value needs to be maintained — consider getting it from external API with default or per-network value(s) as fallback
}

func (s SSV) String() string {
	return fmt.Sprintf("%#v", s)
}

func (s SSV) MarshalYAML() (interface{}, error) {
	aux := &struct {
		Name                      string       `yaml:"Name,omitempty"`
		GenesisDomainType         string       `yaml:"GenesisDomainType,omitempty"`
		AlanDomainType            string       `yaml:"AlanDomainType,omitempty"`
		GenesisEpoch              phase0.Epoch `yaml:"GenesisEpoch,omitempty"`
		RegistrySyncOffset        *big.Int     `yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr      string       `yaml:"RegistryContractAddr,omitempty"`
		Bootnodes                 []string     `yaml:"Bootnodes,omitempty"`
		DiscoveryProtocolID       string       `yaml:"DiscoveryProtocolID,omitempty"`
		AlanForkEpoch             phase0.Epoch `yaml:"AlanForkEpoch,omitempty"`
		MaxValidatorsPerCommittee int          `yaml:"MaxValidatorsPerCommittee,omitempty"`
		TotalEthereumValidators   int          `yaml:"TotalEthereumValidators,omitempty"`
	}{
		Name:                      s.Name,
		GenesisDomainType:         "0x" + hex.EncodeToString(s.GenesisDomainType[:]),
		AlanDomainType:            "0x" + hex.EncodeToString(s.AlanDomainType[:]),
		RegistrySyncOffset:        s.RegistrySyncOffset,
		RegistryContractAddr:      s.RegistryContractAddr.String(),
		Bootnodes:                 s.Bootnodes,
		DiscoveryProtocolID:       "0x" + hex.EncodeToString(s.DiscoveryProtocolID[:]),
		AlanForkEpoch:             s.AlanForkEpoch,
		MaxValidatorsPerCommittee: s.MaxValidatorsPerCommittee,
		TotalEthereumValidators:   s.TotalEthereumValidators,
	}

	return aux, nil
}

func (s *SSV) UnmarshalYAML(unmarshal func(interface{}) error) error {
	aux := &struct {
		Name                      string       `yaml:"Name,omitempty"`
		GenesisDomainType         string       `yaml:"GenesisDomainType,omitempty"`
		AlanDomainType            string       `yaml:"AlanDomainType,omitempty"`
		GenesisEpoch              phase0.Epoch `yaml:"GenesisEpoch,omitempty"`
		RegistrySyncOffset        *big.Int     `yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr      string       `yaml:"RegistryContractAddr,omitempty"`
		Bootnodes                 []string     `yaml:"Bootnodes,omitempty"`
		DiscoveryProtocolID       string       `yaml:"DiscoveryProtocolID,omitempty"`
		AlanForkEpoch             phase0.Epoch `yaml:"AlanForkEpoch,omitempty"`
		MaxValidatorsPerCommittee int          `yaml:"MaxValidatorsPerCommittee,omitempty"`
		TotalEthereumValidators   int          `yaml:"TotalEthereumValidators,omitempty"`
	}{}

	if err := unmarshal(aux); err != nil {
		return err
	}

	genesisDomain, err := hex.DecodeString(strings.TrimPrefix(aux.GenesisDomainType, "0x"))
	if err != nil {
		return fmt.Errorf("decode genesis domain: %w", err)
	}

	var genesisDomainArr spectypes.DomainType
	if len(genesisDomain) != 0 {
		genesisDomainArr = spectypes.DomainType(genesisDomain)
	}

	alanDomain, err := hex.DecodeString(strings.TrimPrefix(aux.AlanDomainType, "0x"))
	if err != nil {
		return fmt.Errorf("decode alan domain: %w", err)
	}

	var alanDomainArr spectypes.DomainType
	if len(alanDomain) != 0 {
		alanDomainArr = spectypes.DomainType(alanDomain)
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
		Name:                      aux.Name,
		GenesisDomainType:         genesisDomainArr,
		AlanDomainType:            alanDomainArr,
		RegistrySyncOffset:        aux.RegistrySyncOffset,
		RegistryContractAddr:      ethcommon.HexToAddress(aux.RegistryContractAddr),
		Bootnodes:                 aux.Bootnodes,
		DiscoveryProtocolID:       discoveryProtocolIDArr,
		AlanForkEpoch:             aux.AlanForkEpoch,
		MaxValidatorsPerCommittee: aux.MaxValidatorsPerCommittee,
		TotalEthereumValidators:   aux.TotalEthereumValidators,
	}

	return nil
}

func (n SSV) AlanForkNetworkName() string {
	return fmt.Sprintf("%s:%s", n.Name, alanForkName)
}

func (n SSV) PastAlanForkAtEpoch(epoch phase0.Epoch) bool {
	return epoch >= n.AlanForkEpoch
}

// DomainTypeAtEpoch returns domain type based on the fork at the given epoch.
func (n SSV) DomainTypeAtEpoch(epoch phase0.Epoch) spectypes.DomainType {
	if n.PastAlanForkAtEpoch(epoch) {
		return n.AlanDomainType
	}
	return n.GenesisDomainType
}

func (n SSV) NextDomainType() spectypes.DomainType {
	return n.AlanDomainType
}
