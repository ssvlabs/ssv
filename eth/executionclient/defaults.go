package executionclient

import (
	"time"
)

const (
	DefaultFinalityDistance           = 32 // Default number of blocks for finality distance
	DefaultConnectionTimeout          = 10 * time.Second
	DefaultHealthInvalidationInterval = 24 * time.Second // TODO: decide on this value, for now choosing the node prober interval but it should probably be a bit less than block interval
	// TODO ALAN: revert
	DefaultHistoricalLogsBatchSize = 200
	defaultLogBuf                  = 8 * 1024
)
