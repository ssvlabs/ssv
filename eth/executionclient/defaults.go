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
	defaultLogBuf                      = 8 * 1024
)
