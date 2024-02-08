package networkconfig

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

func TestNetworkConfig(t *testing.T) {
	yamlConfig := `
Name: "test"
Beacon:
  Parent: "prater"
  Name: "test"
  ForkVersion: "0x12345678"
  MinGenesisTime: 1634025600
  SlotDuration: "12s"
  SlotsPerEpoch: 32
  EpochsPerSyncCommitteePeriod: 256
Domain: "0x87654321"
GenesisEpoch: 123456
RegistrySyncOffset: 321654
RegistryContractAddr: "0xd6b633304Db2DD59ce93753FA55076DA367e5b2c"
Bootnodes:
  - "enr:-LK4QJ9hLJ1csDN4rQoSjlJGE2SvsXOETfcLH8uAVrxlHaELF0u3NeKCTY2eO_X1zy5eEKcHruyaAsGNiyyG4QWUQBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g"
  - "enr:-Li4QO86ZMZr_INMW_WQBsP2jS56yjrHnZXxAUOKJz4_qFPKD1Cr3rghQD2FtXPk2_VPnJUi8BBiMngOGVXC0wTYpJGGAYgqnGSNh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQKNW0Mf-xTXcevRSkZOvoN0Q0T9OkTjGZQyQeOl3bYU3YN0Y3CCE4iDdWRwgg-g"
WhitelistedOperatorKeys:
  - "0x1234567890123456789012345678901234567890123456789012345678901234"
PermissionlessActivationEpoch: 67890
`
	expectedConfig := NetworkConfig{
		Name: "test",
		Beacon: &beaconprotocol.Network{
			Parent:                          "prater",
			Name:                            "test",
			ForkVersionVal:                  [4]byte{0x12, 0x34, 0x56, 0x78},
			MinGenesisTimeVal:               1634025600,
			SlotDurationVal:                 12 * time.Second,
			SlotsPerEpochVal:                32,
			EpochsPerSyncCommitteePeriodVal: 256,
		},
		Domain:               [4]byte{0x87, 0x65, 0x43, 0x21},
		GenesisEpoch:         123456,
		RegistrySyncOffset:   new(big.Int).SetInt64(321654),
		RegistryContractAddr: "0xd6b633304Db2DD59ce93753FA55076DA367e5b2c",
		Bootnodes: []string{
			"enr:-LK4QJ9hLJ1csDN4rQoSjlJGE2SvsXOETfcLH8uAVrxlHaELF0u3NeKCTY2eO_X1zy5eEKcHruyaAsGNiyyG4QWUQBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
			"enr:-Li4QO86ZMZr_INMW_WQBsP2jS56yjrHnZXxAUOKJz4_qFPKD1Cr3rghQD2FtXPk2_VPnJUi8BBiMngOGVXC0wTYpJGGAYgqnGSNh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQKNW0Mf-xTXcevRSkZOvoN0Q0T9OkTjGZQyQeOl3bYU3YN0Y3CCE4iDdWRwgg-g",
		},
		WhitelistedOperatorKeys: []string{
			"0x1234567890123456789012345678901234567890123456789012345678901234",
		},
		PermissionlessActivationEpoch: 67890,
	}

	var unmarshaledConfig NetworkConfig

	require.NoError(t, yaml.Unmarshal([]byte(yamlConfig), &unmarshaledConfig))
	require.EqualValues(t, expectedConfig, unmarshaledConfig)

	require.Equal(t, unmarshaledConfig.Beacon.(*beaconprotocol.Network).ForkVersionVal, unmarshaledConfig.ForkVersion())
	require.Equal(t, unmarshaledConfig.Beacon.(*beaconprotocol.Network).SlotDurationVal, unmarshaledConfig.SlotDuration())
	require.Equal(t, unmarshaledConfig.Beacon.(*beaconprotocol.Network).SlotsPerEpochVal, unmarshaledConfig.SlotsPerEpoch())
	require.Equal(t, time.Unix(int64(unmarshaledConfig.Beacon.(*beaconprotocol.Network).MinGenesisTimeVal), 0), unmarshaledConfig.GetGenesisTime())
}
