package goclient

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/api"
)

// errSingleClient wraps provided error adding more details to it, useful for single-client errors.
func errSingleClient(err error, clientAddr string, routeName string) error {
	return fmt.Errorf("single-client request %s -> %s: %w", clientAddr, routeName, err)
}

// errMultiClient wraps provided error adding more details to it, useful for multi-client errors.
func errMultiClient(err error, routeName string) error {
	return fmt.Errorf("multi-client request -> %s: %w", routeName, err)
}

// isNotFound reports whether err is a beacon-API 404: the beacon node has no such resource (a missing
// route, or no aggregate under a given root), as opposed to a transport or beacon-node failure worth
// retrying.
func isNotFound(err error) bool {
	return responseStatusCode(err) == http.StatusNotFound
}

// responseStatusCode returns the HTTP status of a beacon-API error response, or 0 when err carries none.
// It reads both transports this package speaks: go-eth2-client's typed *api.Error, and the
// *httpStatusError of the hand-rolled Gloas requests.
func responseStatusCode(err error) int {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status
	}
	return 0
}
