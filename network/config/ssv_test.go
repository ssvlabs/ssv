package networkconfig

import (
	"math/big"
	"strings"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNetworkConfig(t *testing.T) {
	yamlConfig := `
Name: test
GenesisDomainType: "0x87654321"
AlanDomainType: "0xfedcba98"
RegistrySyncOffset: "321654"
RegistryContractAddr: 0xd6b633304Db2DD59ce93753FA55076DA367e5b2c
Bootnodes:
    - enr:-LK4QJ9hLJ1csDN4rQoSjlJGE2SvsXOETfcLH8uAVrxlHaELF0u3NeKCTY2eO_X1zy5eEKcHruyaAsGNiyyG4QWUQBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g
    - enr:-Li4QO86ZMZr_INMW_WQBsP2jS56yjrHnZXxAUOKJz4_qFPKD1Cr3rghQD2FtXPk2_VPnJUi8BBiMngOGVXC0wTYpJGGAYgqnGSNh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQKNW0Mf-xTXcevRSkZOvoN0Q0T9OkTjGZQyQeOl3bYU3YN0Y3CCE4iDdWRwgg-g
DiscoveryProtocolID: "0xaabbccddeeff"
AlanForkEpoch: 123123123
MaxValidatorsPerCommittee: 560
TotalEthereumValidators: 1000000
`
	expectedConfig := SSV{
		Name:                 "test",
		GenesisDomainType:    [4]byte{0x87, 0x65, 0x43, 0x21},
		AlanDomainType:       [4]byte{0xfe, 0xdc, 0xba, 0x98},
		RegistrySyncOffset:   new(big.Int).SetInt64(321654),
		RegistryContractAddr: ethcommon.HexToAddress("0xd6b633304Db2DD59ce93753FA55076DA367e5b2c"),
		Bootnodes: []string{
			"enr:-LK4QJ9hLJ1csDN4rQoSjlJGE2SvsXOETfcLH8uAVrxlHaELF0u3NeKCTY2eO_X1zy5eEKcHruyaAsGNiyyG4QWUQBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
			"enr:-Li4QO86ZMZr_INMW_WQBsP2jS56yjrHnZXxAUOKJz4_qFPKD1Cr3rghQD2FtXPk2_VPnJUi8BBiMngOGVXC0wTYpJGGAYgqnGSNh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQKNW0Mf-xTXcevRSkZOvoN0Q0T9OkTjGZQyQeOl3bYU3YN0Y3CCE4iDdWRwgg-g",
		},
		DiscoveryProtocolID:       [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		AlanForkEpoch:             123123123,
		MaxValidatorsPerCommittee: 560,
		TotalEthereumValidators:   1000000,
	}

	var unmarshaledConfig SSV

	require.NoError(t, yaml.Unmarshal([]byte(yamlConfig), &unmarshaledConfig))
	require.EqualValues(t, expectedConfig, unmarshaledConfig)

	marshaledConfig, err := yaml.Marshal(unmarshaledConfig)
	require.NoError(t, err)

	require.Equal(t, strings.TrimSpace(yamlConfig), strings.TrimSpace(string(marshaledConfig)))
}
