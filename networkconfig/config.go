package networkconfig

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var SupportedConfigs = map[string]NetworkConfig{
	Mainnet.Name:      Mainnet,
	Holesky.Name:      Holesky,
	HoleskyStage.Name: HoleskyStage,
	LocalTestnet.Name: LocalTestnet,
	HoleskyE2E.Name:   HoleskyE2E,
}

const alanForkName = "alan"

func GetNetworkConfigByName(name string) (NetworkConfig, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return NetworkConfig{}, fmt.Errorf("network not supported: %v", name)
}

// DomainTypeProvider is an interface for getting the domain type based on the current or given epoch.
type DomainTypeProvider interface {
	DomainType() spectypes.DomainType
	NextDomainType() spectypes.DomainType
	DomainTypeAtEpoch(epoch phase0.Epoch) spectypes.DomainType
}

type NetworkConfig struct {
	Name                 string               `yaml:"Name,omitempty"`
	Beacon               beacon.BeaconNetwork `yaml:"Beacon,omitempty"`
	BeaconConfig         BeaconConfig         `yaml:"BeaconConfig,omitempty"`
	GenesisDomainType    spectypes.DomainType `yaml:"GenesisDomainType,omitempty"`
	AlanDomainType       spectypes.DomainType `yaml:"AlanDomainType,omitempty"`
	GenesisEpoch         phase0.Epoch         `yaml:"GenesisEpoch,omitempty"`
	RegistrySyncOffset   *big.Int             `yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr ethcommon.Address    `yaml:"RegistryContractAddr,omitempty"`
	Bootnodes            []string             `yaml:"Bootnodes,omitempty"`
	DiscoveryProtocolID  [6]byte              `yaml:"DiscoveryProtocolID,omitempty"`

	AlanForkEpoch phase0.Epoch `yaml:"AlanForkEpoch,omitempty"`
}

func (n NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return "<malformed>"
	}

	return string(b)
}

func (n NetworkConfig) MarshalYAML() (interface{}, error) {
	aux := &struct {
		Name                 string               `yaml:"Name,omitempty"`
		Beacon               beacon.BeaconNetwork `yaml:"Beacon,omitempty"`
		GenesisDomainType    string               `yaml:"GenesisDomainType,omitempty"`
		AlanDomainType       string               `yaml:"AlanDomainType,omitempty"`
		GenesisEpoch         phase0.Epoch         `yaml:"GenesisEpoch,omitempty"`
		RegistrySyncOffset   *big.Int             `yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr string               `yaml:"RegistryContractAddr,omitempty"`
		Bootnodes            []string             `yaml:"Bootnodes,omitempty"`
		DiscoveryProtocolID  string               `yaml:"DiscoveryProtocolID,omitempty"`
		AlanForkEpoch        phase0.Epoch         `yaml:"AlanForkEpoch,omitempty"`
	}{
		Name:                 n.Name,
		GenesisDomainType:    "0x" + hex.EncodeToString(n.GenesisDomainType[:]),
		AlanDomainType:       "0x" + hex.EncodeToString(n.AlanDomainType[:]),
		GenesisEpoch:         n.GenesisEpoch,
		RegistrySyncOffset:   n.RegistrySyncOffset,
		RegistryContractAddr: n.RegistryContractAddr.String(),
		Bootnodes:            n.Bootnodes,
		DiscoveryProtocolID:  "0x" + hex.EncodeToString(n.DiscoveryProtocolID[:]),
		AlanForkEpoch:        n.AlanForkEpoch,
	}

	return aux, nil
}

func (n *NetworkConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	aux := &struct {
		Name                 string       `yaml:"Name,omitempty"`
		GenesisDomainType    string       `yaml:"GenesisDomainType,omitempty"`
		AlanDomainType       string       `yaml:"AlanDomainType,omitempty"`
		GenesisEpoch         phase0.Epoch `yaml:"GenesisEpoch,omitempty"`
		RegistrySyncOffset   *big.Int     `yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr string       `yaml:"RegistryContractAddr,omitempty"`
		Bootnodes            []string     `yaml:"Bootnodes,omitempty"`
		DiscoveryProtocolID  string       `yaml:"DiscoveryProtocolID,omitempty"`
		AlanForkEpoch        phase0.Epoch `yaml:"AlanForkEpoch,omitempty"`
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

	*n = NetworkConfig{
		Name:                 aux.Name,
		GenesisDomainType:    genesisDomainArr,
		AlanDomainType:       alanDomainArr,
		GenesisEpoch:         aux.GenesisEpoch,
		RegistrySyncOffset:   aux.RegistrySyncOffset,
		RegistryContractAddr: ethcommon.HexToAddress(aux.RegistryContractAddr),
		Bootnodes:            aux.Bootnodes,
		DiscoveryProtocolID:  discoveryProtocolIDArr,
		AlanForkEpoch:        aux.AlanForkEpoch,
	}

	return nil
}

func (n NetworkConfig) AlanForkNetworkName() string {
	return fmt.Sprintf("%s:%s", n.Name, alanForkName)
}

func (n NetworkConfig) PastAlanFork() bool {
	return n.BeaconConfig.EstimatedCurrentEpoch() >= n.AlanForkEpoch
}

func (n NetworkConfig) PastAlanForkAtEpoch(epoch phase0.Epoch) bool {
	return epoch >= n.AlanForkEpoch
}

// GenesisForkVersion returns the genesis fork version of the network.
func (n NetworkConfig) GenesisForkVersion() [4]byte {
	return n.BeaconConfig.GenesisForkVersion()
}

// SlotDuration returns slot duration
func (n NetworkConfig) SlotDuration() time.Duration {
	return n.BeaconConfig.SlotDuration()
}

// SlotsPerEpoch returns number of slots per one epoch
func (n NetworkConfig) SlotsPerEpoch() phase0.Slot {
	return n.BeaconConfig.SlotsPerEpoch()
}

// GetGenesisTime returns the genesis time in unix time.
func (n NetworkConfig) GetGenesisTime() time.Time {
	return n.BeaconConfig.MinGenesisTime()
}

// DomainType returns current domain type based on the current fork.
func (n NetworkConfig) DomainType() spectypes.DomainType {
	return n.DomainTypeAtEpoch(n.BeaconConfig.EstimatedCurrentEpoch())
}

// DomainTypeAtEpoch returns domain type based on the fork at the given epoch.
func (n NetworkConfig) DomainTypeAtEpoch(epoch phase0.Epoch) spectypes.DomainType {
	if n.PastAlanForkAtEpoch(epoch) {
		return n.AlanDomainType
	}
	return n.GenesisDomainType
}

func (n NetworkConfig) NextDomainType() spectypes.DomainType {
	return n.AlanDomainType
}
