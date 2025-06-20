package executionclient

import (
	"time"
)

// TODO: rename eth1, consider combining with consensus client options

// Options contains config configurations related to Ethereum execution client.
type Options struct {
	Addr                  string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"Execution client WebSocket URL(s). Multiple clients are supported via semicolon-separated URLs (e.g. 'ws://localhost:8546;ws://localhost:8547')"`
	ConnectionTimeout     time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"Timeout for execution client connections"`
	SyncDistanceTolerance uint64        `yaml:"ETH1SyncDistanceTolerance" env:"ETH_1_SYNC_DISTANCE_TOLERANCE" env-default:"5" env-description:"Maximum number of blocks behind head considered in-sync"`

	// HTTP fallback configuration
	HTTPFallbackAddr string `yaml:"ETH1HTTPFallbackAddr" env:"ETH_1_HTTP_FALLBACK_ADDR" env-description:"HTTP endpoint for fallback when WebSocket limits are exceeded. If empty, will attempt to convert WebSocket URL to HTTP"`

	// Adaptive batching
	BatchSize    uint64 `yaml:"ETH1BatchSize" env:"ETH_1_BATCH_SIZE" env-default:"500" env-description:"Initial batch size for log fetching"`
	MinBatchSize uint64 `yaml:"ETH1MinBatchSize" env:"ETH_1_MIN_BATCH_SIZE" env-default:"200" env-description:"Minimum batch size for log fetching"`
	MaxBatchSize uint64 `yaml:"ETH1MaxBatchSize" env:"ETH_1_MAX_BATCH_SIZE" env-default:"2000" env-description:"Maximum batch size for log fetching"`
}
