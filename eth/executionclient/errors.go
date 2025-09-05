package executionclient

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/rpc"
)

var (
	ErrClosed        = fmt.Errorf("closed")
	ErrBadInput      = fmt.Errorf("bad input")
	ErrNothingToSync = errors.New("nothing to sync")
)

// Domain-specific errors.
var (
	errSyncing = fmt.Errorf("syncing")
)

const elResponseErrMsg = "Execution client returned an error"

const (
	// errCodeQueryLimit refers to request exceeding the defined limit
	// https://github.com/ethereum/EIPs/blob/master/EIPS/eip-1474.md
	errCodeQueryLimit = -32005
)

// isRPCQueryLimitError checks if the provided error is a query limit error.
func isRPCQueryLimitError(err error) bool {
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		return rpcErr.ErrorCode() == errCodeQueryLimit
	}

	return false
}

// isSingleClientInterruptedError checks if the provided error represents some sort of interruption
// an ExecutionClient experienced.
func isSingleClientInterruptedError(err error) bool {
	return errors.Is(err, ErrClosed) || errors.Is(err, rpc.ErrClientQuit) || errors.Is(err, context.Canceled)
}

// isMultiClientInterruptedError checks if the provided error represents some sort of interruption
// a MultiClient experienced.
func isMultiClientInterruptedError(err error) bool {
	// Note, if multi-client encountered ErrClosed (it can only come from ExecutionClient) it's safe to
	// assume we are in some sort of shutdown process when thre is no need to use multi-client failover
	// to try and recover from it.
	return errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled)
}
