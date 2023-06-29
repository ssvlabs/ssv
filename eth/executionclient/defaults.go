package executionclient

import (
	"time"
)

const (
	DefaultConnectionTimeout           = 500 * time.Millisecond
	DefaultReconnectionInitialInterval = 1 * time.Second
	DefaultReconnectionMaxInterval     = 64 * time.Second
	DefaultFinalizationOffset          = 8
	DefaultHistoricalLogsBatchSize     = 100
	defaultLogBuf                      = 1024 * 1024
)
