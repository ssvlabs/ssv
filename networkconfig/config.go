package networkconfig

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var SupportedConfigs = map[string]NetworkConfig{
	Mainnet.Name:      Mainnet,
	Holesky.Name:      Holesky,
	HoleskyStage.Name: HoleskyStage,
	JatoV2Stage.Name:  JatoV2Stage,
	JatoV2.Name:       JatoV2,
	LocalTestnet.Name: LocalTestnet,
	HoleskyE2E.Name:   HoleskyE2E,
}

func GetNetworkConfigByName(name string) (NetworkConfig, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return NetworkConfig{}, fmt.Errorf("network not supported: %v", name)
}

type NetworkConfig struct {
	Name                          string                       `json:"name,omitempty" yaml:"Name,omitempty"`
	Beacon                        beaconprotocol.BeaconNetwork `json:"beacon,omitempty" yaml:"Beacon,omitempty"`
	Domain                        spectypes.DomainType         `json:"domain,omitempty" yaml:"Domain,omitempty"`
	GenesisEpoch                  spec.Epoch                   `json:"genesis_epoch,omitempty" yaml:"GenesisEpoch,omitempty"`
	RegistrySyncOffset            *big.Int                     `json:"registry_sync_offset,omitempty" yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr          ethcommon.Address            `json:"registry_contract_addr,omitempty" yaml:"RegistryContractAddr,omitempty"`
	Bootnodes                     []string                     `json:"bootnodes,omitempty" yaml:"Bootnodes,omitempty"`
	WhitelistedOperatorKeys       []string                     `json:"whitelisted_operator_keys,omitempty" yaml:"WhitelistedOperatorKeys,omitempty"`
	PermissionlessActivationEpoch spec.Epoch                   `json:"permissionless_activation_epoch,omitempty" yaml:"PermissionlessActivationEpoch,omitempty"`
}

func (n *NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return "<malformed>"
	}

	return string(b)
}

func (n NetworkConfig) MarshalYAML() (interface{}, error) {
	aux := &struct {
		Name                          string                       `json:"name,omitempty" yaml:"Name,omitempty"`
		Beacon                        beaconprotocol.BeaconNetwork `json:"beacon,omitempty" yaml:"Beacon,omitempty"`
		Domain                        string                       `json:"domain,omitempty" yaml:"Domain,omitempty"`
		GenesisEpoch                  spec.Epoch                   `json:"genesis_epoch,omitempty" yaml:"GenesisEpoch,omitempty"`
		RegistrySyncOffset            *big.Int                     `json:"registry_sync_offset,omitempty" yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr          string                       `json:"registry_contract_addr,omitempty" yaml:"RegistryContractAddr,omitempty"`
		Bootnodes                     []string                     `json:"bootnodes,omitempty" yaml:"Bootnodes,omitempty"`
		WhitelistedOperatorKeys       []string                     `json:"whitelisted_operator_keys,omitempty" yaml:"WhitelistedOperatorKeys,omitempty"`
		PermissionlessActivationEpoch spec.Epoch                   `json:"permissionless_activation_epoch,omitempty" yaml:"PermissionlessActivationEpoch,omitempty"`
	}{
		Name:                          n.Name,
		Beacon:                        n.Beacon,
		Domain:                        "0x" + hex.EncodeToString(n.Domain[:]),
		GenesisEpoch:                  n.GenesisEpoch,
		RegistrySyncOffset:            n.RegistrySyncOffset,
		RegistryContractAddr:          n.RegistryContractAddr.String(),
		Bootnodes:                     n.Bootnodes,
		WhitelistedOperatorKeys:       n.WhitelistedOperatorKeys,
		PermissionlessActivationEpoch: n.PermissionlessActivationEpoch,
	}

	return aux, nil
}

func (n *NetworkConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	aux := &struct {
		Name                          string                  `json:"name,omitempty" yaml:"Name,omitempty"`
		Beacon                        *beaconprotocol.Network `json:"beacon,omitempty" yaml:"Beacon,omitempty"`
		Domain                        string                  `json:"domain,omitempty" yaml:"Domain,omitempty"`
		GenesisEpoch                  spec.Epoch              `json:"genesis_epoch,omitempty" yaml:"GenesisEpoch,omitempty"`
		RegistrySyncOffset            *big.Int                `json:"registry_sync_offset,omitempty" yaml:"RegistrySyncOffset,omitempty"`
		RegistryContractAddr          string                  `json:"registry_contract_addr,omitempty" yaml:"RegistryContractAddr,omitempty"`
		Bootnodes                     []string                `json:"bootnodes,omitempty" yaml:"Bootnodes,omitempty"`
		WhitelistedOperatorKeys       []string                `json:"whitelisted_operator_keys,omitempty" yaml:"WhitelistedOperatorKeys,omitempty"`
		PermissionlessActivationEpoch spec.Epoch              `json:"permissionless_activation_epoch,omitempty" yaml:"PermissionlessActivationEpoch,omitempty"`
	}{}

	if err := unmarshal(aux); err != nil {
		return err
	}

	domain, err := hex.DecodeString(strings.TrimPrefix(aux.Domain, "0x"))
	if err != nil {
		return fmt.Errorf("decode domain: %w", err)
	}

	var domainArr spectypes.DomainType
	if len(domain) != 0 {
		domainArr = spectypes.DomainType(domain)
	}

	*n = NetworkConfig{
		Name:                          aux.Name,
		Beacon:                        aux.Beacon,
		Domain:                        domainArr,
		GenesisEpoch:                  aux.GenesisEpoch,
		RegistrySyncOffset:            aux.RegistrySyncOffset,
		RegistryContractAddr:          ethcommon.HexToAddress(aux.RegistryContractAddr),
		Bootnodes:                     aux.Bootnodes,
		WhitelistedOperatorKeys:       aux.WhitelistedOperatorKeys,
		PermissionlessActivationEpoch: aux.PermissionlessActivationEpoch,
	}

	return nil
}

// ForkVersion returns the fork version of the network.
func (n *NetworkConfig) ForkVersion() [4]byte {
	return n.Beacon.ForkVersion()
}

// SlotDuration returns slot duration
func (n *NetworkConfig) SlotDuration() time.Duration {
	return n.Beacon.SlotDuration()
}

// SlotsPerEpoch returns number of slots per one epoch
func (n *NetworkConfig) SlotsPerEpoch() uint64 {
	return n.Beacon.SlotsPerEpoch()
}

// GetGenesisTime returns the genesis time in unix time.
func (n *NetworkConfig) GetGenesisTime() time.Time {
	return time.Unix(int64(n.Beacon.MinGenesisTime()), 0)
}
