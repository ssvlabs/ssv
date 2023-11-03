package networkconfig

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

const (
	TestNetworkRSAForkEpoch = 1234
	MainnetRSAForkEpoch     = 1 // TODO: Mainnet epoch must be defined!
)

var SupportedConfigs = map[string]NetworkConfig{
	Mainnet.Name:      Mainnet,
	HoleskyStage.Name: HoleskyStage,
	JatoV2Stage.Name:  JatoV2Stage,
	JatoV2.Name:       JatoV2,
	LocalTestnet.Name: LocalTestnet,
}

func GetNetworkConfigByName(name string) (NetworkConfig, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return NetworkConfig{}, fmt.Errorf("network not supported: %v", name)
}

type NetworkConfig struct {
	Name                    string
	Beacon                  beacon.BeaconNetwork
	Domain                  spectypes.DomainType
	GenesisEpoch            spec.Epoch
	RegistrySyncOffset      *big.Int
	RegistryContractAddr    string // TODO: ethcommon.Address
	Bootnodes               []string
	WhitelistedOperatorKeys []string
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
	return time.Unix(int64(n.Beacon.MinGenesisTime()), 0)
}

// RSAMessageFork returns epoch for RSA message fork
func (n NetworkConfig) RSAMessageFork(currentEpoch spec.Epoch) bool {
	switch n.Name {
	case HoleskyStage.Name:
		return true
	case Mainnet.Name:
		return currentEpoch >= MainnetRSAForkEpoch
	case TestNetwork.Name:
		return currentEpoch >= TestNetworkRSAForkEpoch
	default:
		return false
	}
}
