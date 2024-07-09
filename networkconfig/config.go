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
	Name                 string
	Beacon               beacon.BeaconNetwork
	GenesisEpoch         phase0.Epoch
	domainType           spectypes.DomainType
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

func (n NetworkConfig) AlanForked(slot phase0.Slot) bool {
	epoch := n.Beacon.EstimatedEpochAtSlot(slot)
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

// Domain returns current domain type of the network
func (n NetworkConfig) DomainType() spectypes.DomainType {
	if n.AlanForked(n.Beacon.EstimatedCurrentSlot()) {
		copy(n.domainType[2:3], []byte{0x1})
	}
	return n.domainType
}
