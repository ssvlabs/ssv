package executionclient

import (
	"time"
)

// TODO: rename eth1, consider combining with consensus client options

// Options contains config configurations related to Ethereum execution client.
type Options struct {
	Addr                  string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"Execution client WebSocket URL(s). Multiple clients are supported via semicolon-separated URLs (e.g. 'ws://localhost:8546;ws://localhost:8547')"`
	ConnectionTimeout     time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"Timeout for execution client connections"`
	SyncDistanceTolerance uint64        `yaml:"ETH1SyncDistanceTolerance" env:"ETH_1_SYNC_DISTANCE_TOLERANCE" env-default:"5" env-description:"Maximum number of blocks that the execution client can lag behind the current head"`
	HTTPLogClientAddr     string        `yaml:"ETH1HTTPLogClientAddr" env:"ETH_1_HTTP_LOG_CLIENT_ADDR" env-description:"HTTP log client address for the execution client"`

	// Adaptive batch configuration
	BatchSize         uint64 `yaml:"ETH1BatchSize" env:"ETH1BatchSize" env-default:"500" env-description:"Initial batch size for log fetching"`
	MinBatchSize      uint64 `yaml:"ETH1MinBatchSize" env:"ETH1MinBatchSize" env-default:"200" env-description:"Minimum batch size for log fetching"`
	MaxBatchSize      uint64 `yaml:"ETH1MaxBatchSize" env:"ETH1MaxBatchSize" env-default:"2000" env-description:"Maximum batch size for log fetching"`
	HighLogsThreshold uint32 `yaml:"ETH1HighLogsThreshold" env:"ETH1HighLogsThreshold" env-default:"1000" env-description:"Threshold for reducing batch size when too many logs are returned"`
}

// ToBatcherConfig converts Options to BatcherConfig with default ratios.
func (o Options) ToBatcherConfig() BatcherConfig {
	return BatcherConfig{
		InitialSize:       o.BatchSize,
		MinSize:           o.MinBatchSize,
		MaxSize:           o.MaxBatchSize,
		IncreaseRatio:     DefaultIncreaseRatio,
		DecreaseRatio:     DefaultDecreaseRatio,
		HighLogsThreshold: o.HighLogsThreshold,
	}
}
