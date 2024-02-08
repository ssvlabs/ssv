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
	Name                          string                       `json:"name" yaml:"Name"`
	Beacon                        beaconprotocol.BeaconNetwork `json:"beacon" yaml:"Beacon"`
	Domain                        spectypes.DomainType         `json:"domain" yaml:"Domain"`
	GenesisEpoch                  spec.Epoch                   `json:"genesis_epoch" yaml:"GenesisEpoch"`
	RegistrySyncOffset            *big.Int                     `json:"registry_sync_offset" yaml:"RegistrySyncOffset"`
	RegistryContractAddr          ethcommon.Address            `json:"registry_contract_addr" yaml:"RegistryContractAddr"`
	Bootnodes                     []string                     `json:"bootnodes" yaml:"Bootnodes"`
	WhitelistedOperatorKeys       []string                     `json:"whitelisted_operator_keys" yaml:"WhitelistedOperatorKeys"`
	PermissionlessActivationEpoch spec.Epoch                   `json:"permissionless_activation_epoch" yaml:"PermissionlessActivationEpoch"`
}

func (n *NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return "<malformed>"
	}

	return string(b)
}

func (n *NetworkConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	aux := &struct {
		Name   string `json:"name" yaml:"Name"`
		Beacon struct {
			Parent                          spectypes.BeaconNetwork `json:"parent" yaml:"Parent"`
			Name                            string                  `json:"name" yaml:"Name"`
			ForkVersionVal                  string                  `json:"fork_version" yaml:"ForkVersion"`
			MinGenesisTimeVal               uint64                  `json:"min_genesis_time" yaml:"MinGenesisTime"`
			SlotDurationVal                 time.Duration           `json:"slot_duration" yaml:"SlotDuration"`
			SlotsPerEpochVal                uint64                  `json:"slots_per_epoch" yaml:"SlotsPerEpoch"`
			EpochsPerSyncCommitteePeriodVal uint64                  `json:"epochs_per_sync_committee_period" yaml:"EpochsPerSyncCommitteePeriod"`
		} `json:"beacon" yaml:"Beacon"`
		Domain                        string     `json:"domain" yaml:"Domain"`
		GenesisEpoch                  spec.Epoch `json:"genesis_epoch" yaml:"GenesisEpoch"`
		RegistrySyncOffset            *big.Int   `json:"registry_sync_offset" yaml:"RegistrySyncOffset"`
		RegistryContractAddr          string     `json:"registry_contract_addr" yaml:"RegistryContractAddr"` // TODO: ethcommon.Address
		Bootnodes                     []string   `json:"bootnodes" yaml:"Bootnodes"`
		WhitelistedOperatorKeys       []string   `json:"whitelisted_operator_keys" yaml:"WhitelistedOperatorKeys"`
		PermissionlessActivationEpoch spec.Epoch `json:"permissionless_activation_epoch" yaml:"PermissionlessActivationEpoch"`
	}{}

	if err := unmarshal(aux); err != nil {
		return err
	}

	domain, err := hex.DecodeString(strings.TrimPrefix(aux.Domain, "0x"))
	if err != nil {
		return fmt.Errorf("decode domain: %w", err)
	}

	forkVersion, err := hex.DecodeString(strings.TrimPrefix(aux.Beacon.ForkVersionVal, "0x"))
	if err != nil {
		return fmt.Errorf("decode fork version: %w", err)
	}

	contractAddr, err := hex.DecodeString(strings.TrimPrefix(aux.RegistryContractAddr, "0x"))
	if err != nil {
		return fmt.Errorf("decode registry contract address: %w", err)
	}

	*n = NetworkConfig{
		Name: aux.Name,
		Beacon: &beaconprotocol.Network{
			Parent:                          aux.Beacon.Parent,
			Name:                            aux.Beacon.Name,
			ForkVersionVal:                  [4]byte(forkVersion),
			MinGenesisTimeVal:               aux.Beacon.MinGenesisTimeVal,
			SlotDurationVal:                 aux.Beacon.SlotDurationVal,
			SlotsPerEpochVal:                aux.Beacon.SlotsPerEpochVal,
			EpochsPerSyncCommitteePeriodVal: aux.Beacon.EpochsPerSyncCommitteePeriodVal,
		},
		Domain:                        spectypes.DomainType(domain),
		GenesisEpoch:                  aux.GenesisEpoch,
		RegistrySyncOffset:            aux.RegistrySyncOffset,
		RegistryContractAddr:          ethcommon.Address(contractAddr),
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
