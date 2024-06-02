package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var HoleskyStage = NetworkConfig{
	Name:                 "holesky-stage",
	Beacon:               beacon.NewNetwork(spectypes.HoleskyNetwork),
	Domain:               [4]byte{0x00, 0x00, 0x31, 0x99}, // TODO: (Alan) revert
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(84599),
	RegistryContractAddr: "0x0d33801785340072C452b994496B19f196b7eE15",
	Bootnodes: []string{
		"enr:-Li4QCBHHRgmx5fs_9dIFmzZcEpsyyARVCKFhq35cf1VSSoZaK77d8nHmSyEPWOebGRKlYNVXm1RtTTc7TSii90eXseGAY_ZTXR9h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDQiKqmJc2VjcDI1NmsxoQK3StZ1-s-DSBpQECDB4boxFJ56H3rgxgUUeGO2BFAsgYN0Y3CCE4mDdWRwgg-h",
	},
}
