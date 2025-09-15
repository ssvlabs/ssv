package executionclient

import (
	"time"
)

const (
	DefaultReqTimeout                 = 10 * time.Second
	DefaultHealthInvalidationInterval = 24 * time.Second // TODO: decide on this value, for now choosing the node prober interval but it should probably be a bit less than block interval
	DefaultFollowDistance             = 8
	// TODO ALAN: revert
	DefaultHistoricalLogsBatchSize = 200
)
