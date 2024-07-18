package networkconfig

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

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
	Name                 string
	Beacon               beacon.BeaconNetwork
	GenesisDomainType    spectypes.DomainType
	AlanDomainType       spectypes.DomainType
	GenesisEpoch         phase0.Epoch
	RegistrySyncOffset   *big.Int
	RegistryContractAddr string // TODO: ethcommon.Address
	Bootnodes            []string

	AlanForkEpoch phase0.Epoch
}

func (n NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return "<malformed>"
	}

	return string(b)
}

func (n NetworkConfig) PastAlanFork() bool {
	return n.Beacon.EstimatedCurrentEpoch() >= n.AlanForkEpoch
}

func (n NetworkConfig) PastAlanForkAtEpoch(epoch phase0.Epoch) bool {
	return epoch >= n.AlanForkEpoch
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

func (n NetworkConfig) AlanFork() bool {
	// TODO: implement
	return true
}

// DomainType returns current domain type based on the current fork.
func (n NetworkConfig) DomainType() spectypes.DomainType {
	return n.DomainTypeAtEpoch(n.Beacon.EstimatedCurrentEpoch())
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
