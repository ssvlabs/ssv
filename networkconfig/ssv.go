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
	MainnetSSV.Name:      MainnetSSV,
	HoleskySSV.Name:      HoleskySSV,
	HoleskyStageSSV.Name: HoleskyStageSSV,
	LocalTestnetSSV.Name: LocalTestnetSSV,
	HoodiSSV.Name:        HoodiSSV,
	HoodiStageSSV.Name:   HoodiStageSSV,
	SepoliaSSV.Name:      SepoliaSSV,
}

func SSVConfigByName(name string) (*SSV, error) {
	if network, ok := supportedSSVConfigs[name]; ok {
		return network, nil
	}

	return nil, fmt.Errorf("network not supported: %v", name)
}

type SSV struct {
	// Name looks similar to Beacon.Name, however, it's used to differentiate configs on the same
	// beacon network, e.g. holesky, holesky-stage, holesky-e2e, disallowing node start with different config,
	// even if the beacon network is the same.
	Name                 string
	DomainType           spectypes.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
	// MaxF defines max amount of failed operators with which SSV node will continue working.
	// The max amount of operators is inherited from this value using the 3F+1 formula.
	// We currently support only MaxF=4, it should be changed only for experimental testing.
	// IMPORTANT: While it's possible to change it, the codebase is not adapted to the change yet,
	// so it doesn't work out of the box. The support will be added gradually.
	MaxF uint8
	// TotalEthereumValidators value needs to be maintained — consider getting it from external API
	// with default or per-network value(s) as fallback
	TotalEthereumValidators int
	Forks                   SSVForks
}

type SSVForks struct {
	Alan phase0.Epoch
	// GasLimit36Epoch is an epoch when to upgrade from default gas limit value of 30_000_000
	// to 36_000_000.
	GasLimit36 phase0.Epoch
}

func (s *SSV) String() string {
	marshaled, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}

	return string(marshaled)
}

type marshaledConfig struct {
	Name                    string            `json:"name,omitempty" yaml:"Name,omitempty"`
	DomainType              hexutil.Bytes     `json:"domain_type,omitempty" yaml:"DomainType,omitempty"`
	RegistrySyncOffset      *big.Int          `json:"registry_sync_offset,omitempty" yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr    ethcommon.Address `json:"registry_contract_addr,omitempty" yaml:"RegistryContractAddr,omitempty"`
	Bootnodes               []string          `json:"bootnodes,omitempty" yaml:"Bootnodes,omitempty"`
	DiscoveryProtocolID     hexutil.Bytes     `json:"discovery_protocol_id,omitempty" yaml:"DiscoveryProtocolID,omitempty"`
	MaxF                    uint8             `json:"MaxF,omitempty" yaml:"MaxF,omitempty"`
	TotalEthereumValidators int               `json:"total_ethereum_validators,omitempty" yaml:"TotalEthereumValidators,omitempty"`
	Forks                   SSVForks          `json:"forks,omitempty" yaml:"Forks,omitempty"`
}

// Helper method to avoid duplication between MarshalJSON and MarshalYAML
func (s *SSV) marshal() *marshaledConfig {
	return &marshaledConfig{
		Name:                    s.Name,
		DomainType:              s.DomainType[:],
		RegistrySyncOffset:      s.RegistrySyncOffset,
		RegistryContractAddr:    s.RegistryContractAddr,
		Bootnodes:               s.Bootnodes,
		DiscoveryProtocolID:     s.DiscoveryProtocolID[:],
		MaxF:                    s.MaxF,
		TotalEthereumValidators: s.TotalEthereumValidators,
		Forks:                   s.Forks,
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
		Name:                    aux.Name,
		DomainType:              spectypes.DomainType(aux.DomainType),
		RegistrySyncOffset:      aux.RegistrySyncOffset,
		RegistryContractAddr:    aux.RegistryContractAddr,
		Bootnodes:               aux.Bootnodes,
		DiscoveryProtocolID:     [6]byte(aux.DiscoveryProtocolID),
		MaxF:                    aux.MaxF,
		TotalEthereumValidators: aux.TotalEthereumValidators,
		Forks:                   aux.Forks,
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

func (s *SSV) MaxOperators() int {
	const defaultMaxF = 4

	maxF := s.MaxF
	if maxF == 0 {
		maxF = defaultMaxF
	}

	return int(s.calcOperatorCount(maxF))
}

func (s *SSV) calcOperatorCount(f uint8) uint8 {
	// We heavily rely on this formula in the codebase, however, this function is made flexible intentionally
	// to allow experiments on local testnet as there might exist better formulas theoretically
	// (e.g. https://www.anza.xyz/blog/alpenglow-a-new-consensus-for-solana)
	return 3*f + 1
}
