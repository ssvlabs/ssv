package networkconfig

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var SupportedConfigs = map[string]NetworkConfig{
	Mainnet.Name:      Mainnet,
	Holesky.Name:      Holesky,
	HoleskyStage.Name: HoleskyStage,
	LocalTestnet.Name: LocalTestnet,
	HoleskyE2E.Name:   HoleskyE2E,
	Hoodi.Name:        Hoodi,
	HoodiStage.Name:   HoodiStage,
	Sepolia.Name:      Sepolia,
}

func GetNetworkConfigByName(name string) (NetworkConfig, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return NetworkConfig{}, fmt.Errorf("network not supported: %v", name)
}

type NetworkConfig struct {
	Name string
	BeaconConfig
	SSVConfig
}

func (n NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return "<malformed>"
	}

	return string(b)
}

func (n NetworkConfig) NetworkName() string {
	activeFork := n.Forks.ActiveFork(0) // Alan

	return fmt.Sprintf("%s:%s", n.Name, strings.ToLower(activeFork.Name))
}

// ForkVersion returns the fork version of the network.
func (n NetworkConfig) ForkVersion() [4]byte {
	return n.Beacon.ForkVersion()
}

// SlotDurationSec returns slot duration
func (n NetworkConfig) SlotDurationSec() time.Duration {
	return n.Beacon.SlotDurationSec()
}

// SlotsPerEpoch returns number of slots per one epoch
func (n NetworkConfig) SlotsPerEpoch() uint64 {
	return n.Beacon.SlotsPerEpoch()
}

// GetGenesisTime returns the genesis time in unix time.
func (n NetworkConfig) GetGenesisTime() time.Time {
	return time.Unix(n.Beacon.MinGenesisTime(), 0)
}
