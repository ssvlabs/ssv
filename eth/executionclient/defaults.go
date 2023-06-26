package executionclient

import (
	"time"
)

const (
	DefaultFinalizationOffset          = 8
	DefaultConnectionTimeout           = 500 * time.Millisecond
	DefaultReconnectionInitialInterval = 1 * time.Second
	DefaultReconnectionMaxInterval     = 64 * time.Second
)
