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

// errSingleClient wraps provided error adding more details to it, useful for single-client errors.
func (ec *ExecutionClient) errSingleClient(err error, routeName string) error {
	return fmt.Errorf("single-client request %s -> %s: %w", ec.nodeAddr, routeName, err)
}

const (
	// errCodeQueryLimit refers to request exceeding the defined limit
	// https://github.com/ethereum/EIPs/blob/master/EIPS/eip-1474.md
	errCodeQueryLimit = -32005

	// errCodeMethodNotFound is the standard JSON-RPC code for a method the server
	// does not support.
	// https://github.com/ethereum/EIPs/blob/master/EIPS/eip-1474.md
	errCodeMethodNotFound = -32601
)

// isRPCQueryLimitError checks if the provided error is a query limit error.
func isRPCQueryLimitError(err error) bool {
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		return rpcErr.ErrorCode() == errCodeQueryLimit
	}

	return false
}

// isRPCMethodNotFoundError checks if the provided error indicates the server does not
// support the requested RPC method.
func isRPCMethodNotFoundError(err error) bool {
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		return rpcErr.ErrorCode() == errCodeMethodNotFound
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
	// Note, if multi-client encountered ErrClosed (it can only come from ExecutionClient), it is safe to
	// assume we are in some sort of shutdown process when there is no need to use multi-client failover
	// to try and recover from it.
	return errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled)
}
