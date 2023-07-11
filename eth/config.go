package eth

import (
	"time"
)

// TODO: cleanup, rename eth1, consider combining with consensus client options

// ExecutionOptions configurations related to eth1
type ExecutionOptions struct {
	ETH1Addr              string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"ETH1 node WebSocket address"`
	ETH1ConnectionTimeout time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"eth1 node connection timeout"`
	RegistryContractABI   string        // TODO: remove because its support is too complex if we use abigen
	AbiVersion            Version
}

// Version enum to support more than one abi format
// TODO: can we remove it or do we need to support multiple versions?
type Version int64
