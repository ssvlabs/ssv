package executionclient

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubRPCError implements rpc.Error so the JSON-RPC error-code predicates can be exercised without
// a live server.
type stubRPCError struct {
	code int
	msg  string
}

func (e stubRPCError) Error() string  { return e.msg }
func (e stubRPCError) ErrorCode() int { return e.code }

func TestIsRPCMethodNotFoundError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"method not found -32601", stubRPCError{code: errCodeMethodNotFound, msg: "not found"}, true},
		{"method not supported -32004", stubRPCError{code: errCodeMethodNotSupported, msg: "not supported"}, true},
		{"query limit -32005 is not method-not-found", stubRPCError{code: errCodeQueryLimit, msg: "limit"}, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isRPCMethodNotFoundError(tc.err))
		})
	}
}

func TestIsSubdividableLogFetchError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"query-limit code -32005", stubRPCError{code: errCodeQueryLimit, msg: "limit exceeded"}, true},
		{"websocket read limit", errors.New("websocket: read limit exceeded"), true},
		{"response too large (wrapped)", fmt.Errorf("get logs: %w", errors.New("response too large")), true},
		{"http 413", errors.New("413 Request Entity Too Large"), true},
		{"method not found -32601 is not subdividable", stubRPCError{code: errCodeMethodNotFound, msg: "no method"}, false},
		{"plain connection error is not subdividable", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isSubdividableLogFetchError(tc.err))
		})
	}
}
