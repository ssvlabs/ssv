package executionclient

import (
	"time"
)

const (
	DefaultConnectionTimeout           = 10 * time.Second
	DefaultReconnectionInitialInterval = 1 * time.Second
	DefaultReconnectionMaxInterval     = 64 * time.Second
	DefaultHealthInvalidationInterval  = 24 * time.Second // TODO: decide on this value, for now choosing the node prober interval but it should probably be a bit less than block interval
	DefaultFollowDistance              = 8
	// TODO ALAN: revert
	defaultLogBuf = 8 * 1024

	// Adaptive batcher constants
	DefaultBatchSize    = 500
	DefaultMinBatchSize = 200
	DefaultMaxBatchSize = 2000

	DefaultIncreaseRatio = 150 // 150% = 1.5x (50% increase)
	DefaultDecreaseRatio = 70  // 70% = 0.7x (30% decrease)

	DefaultSuccessThreshold = 5

	DefaultLatencyTarget = 500 * time.Millisecond
)
