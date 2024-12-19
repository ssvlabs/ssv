package executionclient

import (
	"time"
)

// TODO: rename eth1, consider combining with consensus client options

// ExecutionOptions contains config configurations related to Ethereum execution client.
type ExecutionOptions struct {
	Addr                  string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"Execution client WebSocket address"`
	ConnectionTimeout     time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"Execution client connection timeout"`
	SyncDistanceTolerance uint64        `yaml:"ETH1SyncDistanceTolerance" env:"ETH_1_SYNC_DISTANCE_TOLERANCE" env-required:"false" env-default:"2" env-description:"The numbers of out-of-sync blocks we can tolerate"`
}
