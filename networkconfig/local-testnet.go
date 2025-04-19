package networkconfig

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var LocalTestnet = NetworkConfig{
	Name: "local-testnet",
	BeaconConfig: BeaconConfig{
		BeaconName:    string(spectypes.PraterNetwork),
		SlotDuration:  spectypes.PraterNetwork.SlotDurationSec(),
		SlotsPerEpoch: phase0.Slot(spectypes.PraterNetwork.SlotsPerEpoch()),
		ForkVersion:   spectypes.PraterNetwork.ForkVersion(),
		GenesisTime:   time.Unix(int64(spectypes.PraterNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, spectypes.JatoV2NetworkID.Byte(), 0x2},
		RegistryContractAddr: "0xC3CD9A0aE89Fff83b71b58b6512D43F8a41f363D",
		Bootnodes: []string{
			"enr:-Li4QLR4Y1VbwiqFYKy6m-WFHRNDjhMDZ_qJwIABu2PY9BHjIYwCKpTvvkVmZhu43Q6zVA29sEUhtz10rQjDJkK3Hd-GAYiGrW2Bh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQJTcI7GHPw-ZqIflPZYYDK_guurp_gsAFF5Erns3-PAvIN0Y3CCE4mDdWRwgg-h",
		},
	},
}
