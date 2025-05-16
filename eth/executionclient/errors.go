package executionclient

import (
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
	// https://github.com/NethermindEth/nethermind/blob/a598851f3621f7e79ea2aff3ff6c90e2cec23ee0/src/Nethermind/Nethermind.JsonRpc/ErrorCodes.cs#L68
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
