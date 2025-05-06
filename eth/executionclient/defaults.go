package executionclient

import (
	"time"
)

const (
	SlotsPerEpoch            = 32
	DefaultFinalityDistance  = SlotsPerEpoch * 2
	DefaultFollowDistance    = 8 // Default follow distance for pre-finality fork
	DefaultFinalityForkEpoch = 0 // Epoch at which to enable finalized blocks from execution client (0 means disabled)

	DefaultConnectionTimeout          = 10 * time.Second
	DefaultHealthInvalidationInterval = 24 * time.Second // TODO: decide on this value, for now choosing the node prober interval but it should probably be a bit less than block interval

	// TODO ALAN: revert
	DefaultHistoricalLogsBatchSize = 200
	defaultLogBuf                  = 8 * 1024
)
