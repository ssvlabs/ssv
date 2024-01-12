package executionclient

import (
	"github.com/ethereum/go-ethereum/rpc"
	"time"
)

const (
	DefaultConnectionTimeout           = 10 * time.Second
	DefaultReconnectionInitialInterval = 1 * time.Second
	DefaultReconnectionMaxInterval     = 64 * time.Second
	DefaultFollowDistance              = 8
	DefaultHistoricalLogsBatchSize     = 5000
	defaultLogBuf                      = 8 * 1024
	defaultRpcGetHeaderArg             = rpc.FinalizedBlockNumber
)
