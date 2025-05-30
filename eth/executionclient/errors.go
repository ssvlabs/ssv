package executionclient

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
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

// isQueryLimitError checks if the provided error is a query limit error.
func isQueryLimitError(err error) bool {
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		return rpcErr.ErrorCode() == errCodeQueryLimit
	}

	if errors.Is(err, websocket.ErrReadLimit) {
		return true
	}

	return false
}
