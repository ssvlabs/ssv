package eth1

import "time"

// Options configurations related to eth1
type Options struct {
	ETH1Addr              string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"ETH1 node WebSocket address"`
	ETH1SyncOffset        string        `yaml:"ETH1SyncOffset" env:"ETH_1_SYNC_OFFSET" env-description:"block number to start the sync from"`
	ETH1ConnectionTimeout time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"eth1 node connection timeout"`
	RegistryContractAddr  string        `yaml:"RegistryContractAddr" env:"REGISTRY_CONTRACT_ADDR_KEY" env-default:"0x9573C41F0Ed8B72f3bD6A9bA6E3e15426A0aa65B" env-description:"registry contract address"`
	RegistryContractABI   string        `yaml:"RegistryContractABI" env:"REGISTRY_CONTRACT_ABI" env-description:"registry contract abi json file"`
	CleanRegistryData     bool          `yaml:"CleanRegistryData" env:"CLEAN_REGISTRY_DATA" env-default:"false" env-description:"cleans registry contract data (validator shares) and forces re-sync"`
	AbiVersion            Version       `yaml:"AbiVersion" env:"ABI_VERSION" env-default:"0" env-description:"smart contract abi version (format)"`
}
