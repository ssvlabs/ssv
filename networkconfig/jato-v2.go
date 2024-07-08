package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var JatoV2 = NetworkConfig{
	Name:                 "jato-v2",
	Beacon:               beacon.NewNetwork(spectypes.PraterNetwork),
	GenesisEpoch:         192100,
	RegistrySyncOffset:   new(big.Int).SetInt64(9203578),
	RegistryContractAddr: "0xC3CD9A0aE89Fff83b71b58b6512D43F8a41f363D",
	Bootnodes: []string{
		// Blox (ssv.network)
		"enr:-Li4QLR4Y1VbwiqFYKy6m-WFHRNDjhMDZ_qJwIABu2PY9BHjIYwCKpTvvkVmZhu43Q6zVA29sEUhtz10rQjDJkK3Hd-GAYiGrW2Bh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQJTcI7GHPw-ZqIflPZYYDK_guurp_gsAFF5Erns3-PAvIN0Y3CCE4mDdWRwgg-h",

		// Taiga (@zkTaiga)
		"enr:-Li4QKQeGeb4yuUkNnVoxT0kjkDcBZN83GRyQkKrQhwqAU7wFitJzjjKIz2IB3Xnwj6hROj7h6Kli1hOmdJ1jVLPDlaGAYj8K5Hvh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhJbmcQeJc2VjcDI1NmsxoQIYVg92mRyqn519Og6VA6fdgqeFxKgQO87IX64zJcmqhoN0Y3CCE4iDdWRwgg-g",

		// 0NEinfra (www.0neinfra.io)
		"enr:-Li4QMCh155TJ9K7xL_2gnmyi9IPQkuqRLG8U5rW1S2wmpukDrFX7WaThIihMRWWizsp-GILZIeqa0nZrmV3tVOVHPKGAYj8ICi3h2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhDaTS0mJc2VjcDI1NmsxoQMoexiUvxbufU3x0fAQXtbMzM9XIq0Es16K0Hkfa682k4N0Y3CCE4iDdWRwgg-g",

		// CryptoManufaktur (cryptomanufaktur.io)
		"enr:-Li4QAOtGzianKrNVqTQtH23DtpZ6UY8nZNvUthzoeD7ACgFU_a8GSJXXoWM2Q_mSEBlU6AZIUoICADvV2g65RNDn1aGAYj9A33Bh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQLbXMJi_Pq3imTq11EwH8MbxmXlHYvH2Drz_rsqP1rNyoN0Y3CCE4iDdWRwgg-g",
	},
}
