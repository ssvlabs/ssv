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
	ErrNothingToSync = fmt.Errorf("nothing to sync")
	ErrSyncing       = fmt.Errorf("syncing")
)

// errWithDetails wraps provided error adding more details to it.
func (ec *ExecutionClient) errWithDetails(err error, method string) error {
	return fmt.Errorf("%s -> %s: %w", method, ec.nodeAddr, err)
}

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

// isInterruptedError checks if the provided error represents some sort of interruption.
func isInterruptedError(err error) bool {
	return errors.Is(err, ErrClosed) || errors.Is(err, rpc.ErrClientQuit) || errors.Is(err, context.Canceled)
}
