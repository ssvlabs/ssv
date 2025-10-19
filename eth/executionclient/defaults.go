package executionclient

import (
	"time"
)

const (
	DefaultReqTimeout    = 10 * time.Second
	DefaultReqRetryDelay = 10 * time.Second

	DefaultFollowDistance = 8

	DefaultHealthInvalidationInterval = 10 * time.Second

	DefaultSyncDistanceTolerance = 5
)
