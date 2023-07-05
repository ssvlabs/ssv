package eventdispatcher

import (
	"context"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type executionClient interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logCh <-chan ethtypes.Log, fetchErrCh <-chan error, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) <-chan ethtypes.Log
}
