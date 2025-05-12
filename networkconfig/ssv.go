package networkconfig

import (
	"encoding/json"
	"fmt"
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./ssv_mock.go -source=./ssv.go

var supportedSSVConfigs = map[string]SSVConfig{
	Mainnet.Name:      Mainnet.SSVConfig,
	Holesky.Name:      Holesky.SSVConfig,
	HoleskyStage.Name: HoleskyStage.SSVConfig,
	LocalTestnet.Name: LocalTestnet.SSVConfig,
	HoleskyE2E.Name:   HoleskyE2E.SSVConfig,
	Hoodi.Name:        Hoodi.SSVConfig,
	HoodiStage.Name:   HoodiStage.SSVConfig,
	Sepolia.Name:      Sepolia.SSVConfig,
}

func GetSSVConfigByName(name string) (SSVConfig, error) {
	if network, ok := supportedSSVConfigs[name]; ok {
		return network, nil
	}

	return SSVConfig{}, fmt.Errorf("network not supported: %v", name)
}

type SSV interface {
	GetDomainType() spectypes.DomainType
}

type SSVConfig struct {
	DomainType           spectypes.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr string // TODO: ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
}

func (s SSVConfig) String() string {
	marshaled, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}

	return string(marshaled)
}

func (s SSVConfig) GetDomainType() spectypes.DomainType {
	return s.DomainType
}
