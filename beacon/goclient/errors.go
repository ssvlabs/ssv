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

// isNotFound reports whether err is a beacon-API 404, over either transport this package speaks:
// go-eth2-client's typed *api.Error, and the *httpStatusError the hand-rolled Gloas endpoints return.
// Callers care about the status, not which client produced it — a 404 means the beacon node has no
// such resource (a missing route, or no aggregate under a given root), as opposed to a transport or
// beacon-node failure worth retrying.
func isNotFound(err error) bool {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.status == http.StatusNotFound
}
