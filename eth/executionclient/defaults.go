package executionclient

import (
	"time"
)

const (
	// DefaultFollowDistance is the default follow distance for the pre-finality fork.
	// This is the number of blocks that the execution client will follow behind the head of the chain.
	DefaultFollowDistance = 8

	DefaultConnectionTimeout          = 10 * time.Second
	DefaultHealthInvalidationInterval = 24 * time.Second // TODO: decide on this value, for now choosing the node prober interval but it should probably be a bit less than block interval

	// TODO ALAN: revert
	DefaultHistoricalLogsBatchSize = 200
	defaultLogBuf                  = 8 * 1024
)
