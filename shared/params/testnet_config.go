package params

// UseTestnetConfig sets config for the testnet
func UseTestnetConfig() {
	ssvConfig = TestnetConfig()
}

// TestnetConfig defines the config for the testnet.
func TestnetConfig() *SsvNetworkConfig {
	return testnetSsvConfig
}

var testnetSsvConfig = &SsvNetworkConfig{
	OperatorContractAddress: "0x555fe4a050Bb5f392fD80dCAA2b6FCAf829f21e9",
}
