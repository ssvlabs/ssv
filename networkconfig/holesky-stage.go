package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var HoleskyStage = NetworkConfig{
	Name:                 "holesky-stage",
	Beacon:               beacon.NewNetwork(spectypes.HoleskyNetwork),
	Domain:               [4]byte{0x00, 0x00, 0x31, 0x12},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(84599),
	RegistryContractAddr: "0x0d33801785340072C452b994496B19f196b7eE15",
	Bootnodes: []string{
		"enr:-Li4QP4F8hNKamUYYuVsQXNGB4hgQM9t-12_--I9y_Z4N-PCR3cjHuqJJ-FPbG_DqxSyWazkNtP_7zHLbSRDnDGBFHaGAY_E6m1xh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDQiKqmJc2VjcDI1NmsxoQKnAiuSlgSR8asjCH0aYoVKM8uPbi4noFuFHZHaAHqknYN0Y3CCE4mDdWRwgg-h",
	},
}
