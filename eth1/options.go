package eth1

import "time"

// Options configurations related to eth1
type Options struct {
	ETH1Addr              string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"ETH1 node WebSocket address"`
	ETH1ConnectionTimeout time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"eth1 node connection timeout"`
	CleanRegistryData     bool          `yaml:"CleanRegistryData" env:"CLEAN_REGISTRY_DATA" env-default:"false" env-description:"cleans registry contract data (validator shares) and forces re-sync"`
	RegistryContractABI   string
	AbiVersion            Version
}
