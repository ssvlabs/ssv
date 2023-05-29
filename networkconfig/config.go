package networkconfig

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

var SupportedConfigs = map[string]NetworkConfig{
	Mainnet.Name:     Mainnet,
	JatoV2Stage.Name: JatoV2Stage,
}

func GetNetworkByName(name string) (NetworkConfig, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return NetworkConfig{}, fmt.Errorf("network not supported: %v", name)
}

type NetworkConfig struct {
	Name                 string
	BeaconNetwork        spectypes.BeaconNetwork
	Domain               spectypes.DomainType
	GenesisEpoch         spec.Epoch
	ETH1SyncOffset       *big.Int
	RegistryContractAddr string
	Bootnodes            []string
}

func (n NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return "<malformed>"
	}

	return string(b)
}

// ForkVersion returns the fork version of the network.
func (n NetworkConfig) ForkVersion() [4]byte {
	return n.BeaconNetwork.ForkVersion()
}

// SlotDurationSec returns slot duration
func (n NetworkConfig) SlotDurationSec() time.Duration {
	return n.BeaconNetwork.SlotDurationSec()
}

// SlotsPerEpoch returns number of slots per one epoch
func (n NetworkConfig) SlotsPerEpoch() uint64 {
	return n.BeaconNetwork.SlotsPerEpoch()
}
