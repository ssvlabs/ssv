package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var HoleskyStage = NetworkConfig{
	Name: "holesky-stage",
	BeaconConfig: BeaconConfig{
		BeaconName:    string(spectypes.HoleskyNetwork),
		SlotDuration:  spectypes.HoleskyNetwork.SlotDurationSec(),
		SlotsPerEpoch: phase0.Slot(spectypes.HoleskyNetwork.SlotsPerEpoch()),
		ForkVersion:   spectypes.HoleskyNetwork.ForkVersion(),
		GenesisTime:   time.Unix(int64(spectypes.HoleskyNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
	},
	SSVConfig: SSVConfig{
		DomainType:           [4]byte{0x00, 0x00, 0x31, 0x13},
		RegistrySyncOffset:   new(big.Int).SetInt64(84599),
		RegistryContractAddr: ethcommon.HexToAddress("0x0d33801785340072C452b994496B19f196b7eE15"),
		DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
		Bootnodes: []string{
			// Public bootnode:
			// "enr:-Ja4QDYHVgUs9NvlMqq93ot6VNqbmrIlMrwKnq4X3DPRgyUNB4ospDp8ubMvsf-KsgqY8rzpZKy4GbE1DLphabpRBc-GAY_diLjngmlkgnY0gmlwhDQrLYqJc2VjcDI1NmsxoQKnAiuSlgSR8asjCH0aYoVKM8uPbi4noFuFHZHaAHqknYNzc3YBg3RjcIITiYN1ZHCCD6E",

			// Private bootnode:
			"enr:-Ja4QDRUBjWOvVfGxpxvv3FqaCy3psm7IsKu5ETb1GXiexGYDFppD33t7AHRfmQddoAkBiyb7pt4t7ZN0sNB9CsW4I-GAZGOmChMgmlkgnY0gmlwhAorXxuJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
		},
	},
}
