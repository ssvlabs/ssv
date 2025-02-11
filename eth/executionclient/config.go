package executionclient

import (
	"time"
)

// TODO: rename eth1, consider combining with consensus client options

// ExecutionOptions contains config configurations related to Ethereum execution client.
type ExecutionOptions struct {
	Addr                  string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"Execution client WebSocket address. Supports multiple semicolon separated addresses. ex: ws://localhost:8546;ws://localhost:8547"`
	ConnectionTimeout     time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"Execution client connection timeout"`
	SyncDistanceTolerance uint64        `yaml:"ETH1SyncDistanceTolerance" env:"ETH_1_SYNC_DISTANCE_TOLERANCE" env-default:"5" env-description:"The number of out-of-sync blocks we can tolerate"`
}
