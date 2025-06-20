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
	DefaultBatchSize = 500
	MinBatchSize     = 200
	MaxBatchSize     = 2000

	batchIncreaseRatio = 1.5
	batchDecreaseRatio = 0.7
	successThreshold   = 5
	latencyTarget      = 500 * time.Millisecond
)
