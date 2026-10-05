package goclient

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/stretchr/testify/require"
)

// responseStatusCode reads the status over both transports, through wrapping, and is 0 for an error
// without one.
func TestResponseStatusCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"no status", errors.New("connection refused"), 0},
		{"go-eth2-client error", &api.Error{StatusCode: http.StatusServiceUnavailable}, http.StatusServiceUnavailable},
		{"hand-rolled error", &httpStatusError{status: http.StatusMethodNotAllowed}, http.StatusMethodNotAllowed},
		{"wrapped hand-rolled error", fmt.Errorf("produce: %w", &httpStatusError{status: http.StatusNotFound}), http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, responseStatusCode(tt.err))
			require.Equal(t, tt.want == http.StatusNotFound, isNotFound(tt.err))
		})
	}
}
