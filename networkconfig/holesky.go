package networkconfig

import (
	"math"
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var HoleskySSV = &SSV{
	Name:       "holesky",
	DomainType: spectypes.DomainType{0x0, 0x0, 0x5, 0x2},
	// The naive Boole domain (Alan+1 = {0,0,5,3}) collides with hoodi's live Alan DomainType,
	// which shares this ad-hoc 0x05 lane; staying adjacent would also re-collide when hoodi
	// schedules its own next fork. Move holesky's Boole domain clear of the hoodi 0x02–0x04
	// cluster (and sepolia's 0x69–0x6A). Safe to change while Boole is unscheduled here.
	// Uniqueness across all built-in networks is enforced by TestBuiltinNetworkDomainsAreUnique.
	NextDomainType:       spectypes.DomainType{0x0, 0x0, 0x5, 0x22},
	RegistrySyncOffset:   new(big.Int).SetInt64(181612),
	RegistryContractAddr: ethcommon.HexToAddress("0x38A4794cCEd47d3baf7370CcC43B560D3a1beEFA"),
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QKFD3u5tZob7xukp-JKX9QJMFqqI68cItsE4tBbhsOyDR0M_1UUjb35hbrqvTP3bnXO_LnKh-jNLTeaUqN4xiduGAZKaP_sagmlkgnY0gmlwhDb0fh6Jc2VjcDI1NmsxoQMw_H2anuiqP9NmEaZwbUfdvPFog7PvcKmoVByDa576SINzc3YBg3RjcIITioN1ZHCCD6I",
	},
	TotalEthereumValidators: 1757795, // active_validators from https://holesky.beaconcha.in/index/data on Nov 20, 2024
	Forks: SSVForks{
		Boole: math.MaxUint64,
	},
}
