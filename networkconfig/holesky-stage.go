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
		"enr:-Li4QO86ZMZr_INMW_WQBsP2jS56yjrHnZXxAUOKJz4_qFPKD1Cr3rghQD2FtXPk2_VPnJUi8BBiMngOGVXC0wTYpJGGAYgqnGSNh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQKNW0Mf-xTXcevRSkZOvoN0Q0T9OkTjGZQyQeOl3bYU3YN0Y3CCE4iDdWRwgg-g",
	},
	WhitelistedOperatorKeys: []string{},
}
