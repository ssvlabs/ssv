package networkconfig

import (
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var HoleskyStage = NetworkConfig{
	Name:                 "holesky-stage",
	Beacon:               beacon.NewNetwork(spectypes.HoleskyNetwork),
	Domain:               [4]byte{0x00, 0x00, 0x31, 0x12},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(84599),
	RegistryContractAddr: "0x0d33801785340072C452b994496B19f196b7eE15",
	Bootnodes: []string{
		// TODO: REVERT!
		"enr:-Li4QD7kSYtiRPbHVwU1PT3ty_WUbG751WY0yLWJm_28MtUHeJ9u09f7hEBG1Pu7ZbCD-xp7cIDNRtO6GlO48CYEgryGAZAGURq7h2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhMPJOTWJc2VjcDI1NmsxoQOepw1N4KkK4zhAkF0tv2HEATfhibVCbXIhmXuAmUDwRoN0Y3CCE4iDdWRwgg-g",
	},
	WhitelistedOperatorKeys:       []string{},
	PermissionlessActivationEpoch: 10560,
}
