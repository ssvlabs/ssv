package executionclient

import (
	"time"
)

// TODO: rename eth1, consider combining with consensus client options

// ExecutionOptions contains config configurations related to Ethereum execution client.
type ExecutionOptions struct {
	Addr                string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"Execution client WebSocket address"`
	ConnectionTimeout   time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"Execution client connection timeout"`
	AllowUnsyncedBlocks uint64        `yaml:"AllowUnsyncedBlocksCount" env:"ALLOW_UNSYNCED_BLOCKS" env-default:"2" env-description:"The numbers of blocks we are allowed to lag behind"`
}
