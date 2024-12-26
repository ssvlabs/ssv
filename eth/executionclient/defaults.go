package executionclient

import (
	"time"
)

const (
	DefaultConnectionTimeout           = 10 * time.Second
	DefaultReconnectionInitialInterval = 1 * time.Second
	DefaultReconnectionMaxInterval     = 64 * time.Second
	DefaultFollowDistance              = 8
	// TODO ALAN: revert
	DefaultHistoricalLogsBatchSize = 500
	defaultLogBuf                  = 8 * 1024
	maxReconnectionAttempts        = 5000
	reconnectionBackoffFactor      = 2
	healthCheckInterval            = 30 * time.Second
)
