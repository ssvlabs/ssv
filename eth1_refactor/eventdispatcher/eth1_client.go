package eventdispatcher

import (
	"context"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type eth1Client interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs []ethtypes.Log, lastBlock uint64, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) <-chan ethtypes.Log
}
