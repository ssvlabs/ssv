package networkconfig

import (
	"encoding/json"
	"fmt"
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./ssv_mock.go -source=./ssv.go

var SupportedSSVConfigs = map[string]SSVConfig{
	Mainnet.SSVName:      Mainnet,
	Holesky.SSVName:      Holesky,
	HoleskyStage.SSVName: HoleskyStage,
	LocalTestnet.SSVName: LocalTestnet,
	HoleskyE2E.SSVName:   HoleskyE2E,
	Hoodi.SSVName:        Hoodi,
	HoodiStage.SSVName:   HoodiStage,
	Sepolia.SSVName:      Sepolia,
}

func GetSSVConfigByName(name string) (SSVConfig, error) {
	if network, ok := SupportedSSVConfigs[name]; ok {
		return network, nil
	}

	return SSVConfig{}, fmt.Errorf("network not supported: %v", name)
}

type SSV interface {
	GetDomainType() spectypes.DomainType
}

type SSVConfig struct {
	SSVName              string
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
