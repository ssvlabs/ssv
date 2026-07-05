package networkconfig

import (
	"encoding/json"
	"fmt"
	"math"
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
	NextDomainType       spectypes.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
	// TotalEthereumValidators value needs to be maintained — consider getting it from external API
	// with default or per-network value(s) as fallback
	TotalEthereumValidators int
	Forks                   SSVForks
}

type SSVForks struct {
	Boole phase0.Epoch `yaml:"Boole" json:"Boole"`
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
	NextDomainType          hexutil.Bytes     `json:"next_domain_type,omitempty" yaml:"NextDomainType,omitempty"`
	RegistrySyncOffset      *big.Int          `json:"registry_sync_offset,omitempty" yaml:"RegistrySyncOffset,omitempty"`
	RegistryContractAddr    ethcommon.Address `json:"registry_contract_addr,omitempty" yaml:"RegistryContractAddr,omitempty"`
	Bootnodes               []string          `json:"bootnodes,omitempty" yaml:"Bootnodes,omitempty"`
	DiscoveryProtocolID     hexutil.Bytes     `json:"discovery_protocol_id,omitempty" yaml:"DiscoveryProtocolID,omitempty"`
	TotalEthereumValidators int               `json:"total_ethereum_validators,omitempty" yaml:"TotalEthereumValidators,omitempty"`
	// Forks is a pointer so unmarshaling can distinguish "forks block absent" (nil) from
	// "forks block present with explicit zero values" (non-nil, e.g. Boole: 0).
	Forks *SSVForks `json:"forks,omitempty" yaml:"Forks,omitempty"`
}

// Helper method to avoid duplication between MarshalJSON and MarshalYAML
func (s *SSV) marshal() *marshaledConfig {
	return &marshaledConfig{
		Name:                    s.Name,
		DomainType:              s.DomainType[:],
		NextDomainType:          s.NextDomainType[:],
		RegistrySyncOffset:      s.RegistrySyncOffset,
		RegistryContractAddr:    s.RegistryContractAddr,
		Bootnodes:               s.Bootnodes,
		DiscoveryProtocolID:     s.DiscoveryProtocolID[:],
		TotalEthereumValidators: s.TotalEthereumValidators,
		Forks:                   &s.Forks,
	}
}

func (s *SSV) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.marshal())
}

func (s *SSV) MarshalYAML() (any, error) {
	return s.marshal(), nil
}

// Helper method to avoid duplication between UnmarshalJSON and UnmarshalYAML
func (s *SSV) unmarshalFromConfig(aux marshaledConfig) error {
	if len(aux.DomainType) != 4 {
		return fmt.Errorf("invalid domain type length: expected 4 bytes, got %d", len(aux.DomainType))
	}
	if len(aux.NextDomainType) != 0 && len(aux.NextDomainType) != 4 {
		return fmt.Errorf("invalid next domain type length: expected 4 bytes, got %d", len(aux.NextDomainType))
	}

	if len(aux.NextDomainType) == 0 {
		aux.NextDomainType = aux.DomainType
	}

	if len(aux.DiscoveryProtocolID) != 6 {
		return fmt.Errorf("invalid discovery protocol ID length: expected 6 bytes, got %d", len(aux.DiscoveryProtocolID))
	}

	// If the config has no "forks" block at all (e.g. a stale custom-network YAML/JSON
	// predating the Boole fork), default Boole to "never activates" rather than letting it
	// zero-value to epoch 0 (fork-at-genesis). An explicit "forks: {Boole: 0}" is still
	// honored verbatim. This protects custom-network operators from silently jumping to
	// post-fork behavior when they haven't opted in.
	forks := SSVForks{Boole: math.MaxUint64}
	if aux.Forks != nil {
		forks = *aux.Forks
	}

	*s = SSV{
		Name:                    aux.Name,
		DomainType:              spectypes.DomainType(aux.DomainType),
		NextDomainType:          spectypes.DomainType(aux.NextDomainType),
		RegistrySyncOffset:      aux.RegistrySyncOffset,
		RegistryContractAddr:    aux.RegistryContractAddr,
		Bootnodes:               aux.Bootnodes,
		DiscoveryProtocolID:     [6]byte(aux.DiscoveryProtocolID),
		TotalEthereumValidators: aux.TotalEthereumValidators,
		Forks:                   forks,
	}

	return nil
}

func (s *SSV) UnmarshalYAML(unmarshal func(any) error) error {
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
