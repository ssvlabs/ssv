package networkconfig

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var supportedSSVConfigs = map[string]*SSV{
	MainnetSSV.SSVName:      MainnetSSV,
	HoleskySSV.SSVName:      HoleskySSV,
	HoleskyStageSSV.SSVName: HoleskyStageSSV,
	LocalTestnetSSV.SSVName: LocalTestnetSSV,
	HoleskyE2ESSV.SSVName:   HoleskyE2ESSV,
	HoodiSSV.SSVName:        HoodiSSV,
	HoodiStageSSV.SSVName:   HoodiStageSSV,
	SepoliaSSV.SSVName:      SepoliaSSV,
}

func SSVConfigByName(name string) (*SSV, error) {
	if network, ok := supportedSSVConfigs[name]; ok {
		return network, nil
	}

	return nil, fmt.Errorf("network not supported: %v", name)
}

type SSV struct {
	// SSVName looks similar to Beacon.BeaconNetwork, however, it's used to differentiate configs on the same
	// beacon network, e.g. holesky, holesky-stage, holesky-e2e, disallowing node start with different config,
	// even if the beacon network is the same.
	SSVName              string
	DomainType           spectypes.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
	// TotalEthereumValidators value needs to be maintained — consider getting it from external API
	// with default or per-network value(s) as fallback
	TotalEthereumValidators int
	// GasLimit36Epoch is an epoch when to upgrade from default gas limit value of 30_000_000
	// to 36_000_000.
	GasLimit36Epoch phase0.Epoch
	SSVForks        []SSVFork // name has SSV prefix because of embedding
}

func (s *SSV) String() string {
	marshaled, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}

	return string(marshaled)
}

type marshaledConfig struct {
	SSVName                 string            `json:"ssv_name,omitempty" yaml:"ssv_name,omitempty"`
	DomainType              hexutil.Bytes     `json:"DomainType,omitempty" yaml:"DomainType,omitempty"`
	RegistrySyncOffset      *big.Int          `json:"RegistrySyncOffset,omitempty" yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr    ethcommon.Address `json:"RegistryContractAddr,omitempty" yaml:"RegistryContractAddr,omitempty"`
	Bootnodes               []string          `json:"Bootnodes,omitempty" yaml:"Bootnodes,omitempty"`
	DiscoveryProtocolID     hexutil.Bytes     `json:"DiscoveryProtocolID,omitempty" yaml:"DiscoveryProtocolID,omitempty"`
	TotalEthereumValidators int               `json:"TotalEthereumValidators,omitempty" yaml:"TotalEthereumValidators,omitempty"`
	GasLimit36Epoch         phase0.Epoch      `json:"GasLimit36Epoch,omitempty" yaml:"GasLimit36Epoch,omitempty"`
	SSVForks                []SSVFork         `json:"SSVForks,omitempty" yaml:"SSVForks,omitempty"`
}

// Helper method to avoid duplication between MarshalJSON and MarshalYAML
func (s *SSV) marshal() *marshaledConfig {
	return &marshaledConfig{
		SSVName:                 s.SSVName,
		DomainType:              s.DomainType[:],
		RegistrySyncOffset:      s.RegistrySyncOffset,
		RegistryContractAddr:    s.RegistryContractAddr,
		Bootnodes:               s.Bootnodes,
		DiscoveryProtocolID:     s.DiscoveryProtocolID[:],
		TotalEthereumValidators: s.TotalEthereumValidators,
		GasLimit36Epoch:         s.GasLimit36Epoch,
		SSVForks:                s.SSVForks,
	}
}

func (s *SSV) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.marshal())
}

func (s *SSV) MarshalYAML() (interface{}, error) {
	return s.marshal(), nil
}

// Helper method to avoid duplication between UnmarshalJSON and UnmarshalYAML
func (s *SSV) unmarshalFromConfig(aux marshaledConfig) error {
	if len(aux.DomainType) != 4 {
		return fmt.Errorf("invalid domain type length: expected 4 bytes, got %d", len(aux.DomainType))
	}

	if len(aux.DiscoveryProtocolID) != 6 {
		return fmt.Errorf("invalid discovery protocol ID length: expected 6 bytes, got %d", len(aux.DiscoveryProtocolID))
	}

	*s = SSV{
		SSVName:                 aux.SSVName,
		DomainType:              spectypes.DomainType(aux.DomainType),
		RegistrySyncOffset:      aux.RegistrySyncOffset,
		RegistryContractAddr:    aux.RegistryContractAddr,
		Bootnodes:               aux.Bootnodes,
		DiscoveryProtocolID:     [6]byte(aux.DiscoveryProtocolID),
		TotalEthereumValidators: aux.TotalEthereumValidators,
		GasLimit36Epoch:         aux.GasLimit36Epoch,
		SSVForks:                aux.SSVForks,
	}

	return nil
}

func (s *SSV) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var aux marshaledConfig
	if err := unmarshal(&aux); err != nil {
		return err
	}

	return s.unmarshalFromConfig(aux)
}

func (s *SSV) UnmarshalJSON(data []byte) error {
	var aux marshaledConfig
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	return s.unmarshalFromConfig(aux)
}

type SSVFork struct {
	Name  string
	Epoch phase0.Epoch
}

func (s *SSV) ForkAtEpoch(epoch phase0.Epoch) SSVFork {
	if len(s.SSVForks) == 0 {
		panic("misconfiguration: config must have SSV forks")
	}

	var currentFork *SSVFork

	for _, fork := range s.SSVForks {
		if epoch >= fork.Epoch && (currentFork == nil || fork.Epoch > currentFork.Epoch) {
			currentFork = &fork
		}
	}

	if currentFork == nil {
		panic(fmt.Sprintf("misconfiguration: no forks matching epoch %d: ", epoch))
	}

	return *currentFork
}
