package executionclient

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// isInterruptedError checks if the provided error represents some sort of interruption.
func isInterruptedError(err error) bool {
	return errors.Is(err, ErrClosed) || errors.Is(err, rpc.ErrClientQuit) || errors.Is(err, context.Canceled)
}

// isWSReadLimitError checks if the given error is related to exceeding the read limit in a WebSocket connection.
func isWSReadLimitError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "websocket: read limit exceeded")
}
