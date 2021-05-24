package params

var ssvConfig = TestnetConfig()

// SsvConfig retrieves ssv config.
func SsvConfig() *SsvNetworkConfig {
	return ssvConfig
}
