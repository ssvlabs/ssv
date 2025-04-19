package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var Holesky = NetworkConfig{
	Name: "holesky",
	BeaconConfig: BeaconConfig{
		BeaconName:    string(spectypes.HoleskyNetwork),
		SlotDuration:  spectypes.HoleskyNetwork.SlotDurationSec(),
		SlotsPerEpoch: phase0.Slot(spectypes.HoleskyNetwork.SlotsPerEpoch()),
		ForkVersion:   spectypes.HoleskyNetwork.ForkVersion(),
		GenesisTime:   time.Unix(int64(spectypes.HoleskyNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x2},
		RegistrySyncOffset:   new(big.Int).SetInt64(181612),
		RegistryContractAddr: ethcommon.HexToAddress("0x38A4794cCEd47d3baf7370CcC43B560D3a1beEFA"),
		DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
		Bootnodes: []string{
			// SSV Labs
			"enr:-Ja4QKFD3u5tZob7xukp-JKX9QJMFqqI68cItsE4tBbhsOyDR0M_1UUjb35hbrqvTP3bnXO_LnKh-jNLTeaUqN4xiduGAZKaP_sagmlkgnY0gmlwhDb0fh6Jc2VjcDI1NmsxoQMw_H2anuiqP9NmEaZwbUfdvPFog7PvcKmoVByDa576SINzc3YBg3RjcIITioN1ZHCCD6I",
		},
		TotalEthereumValidators: 1757795, // active_validators from https://holesky.beaconcha.in/index/data on Nov 20, 2024
	},
}
