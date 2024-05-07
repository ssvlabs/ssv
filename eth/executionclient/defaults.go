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
	DefaultHistoricalLogsBatchSize = 1000
	defaultLogBuf                  = 8 * 1024
)
