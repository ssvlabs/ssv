package goclient

import (
	"fmt"
)

// errSingleClient wraps provided error adding more details to it, useful for single-client errors.
func errSingleClient(err error, clientAddr string, method string) error {
	return fmt.Errorf("single-client request %s -> %s: %w", clientAddr, method, err)
}

// errMultiClient wraps provided error adding more details to it, useful for multi-client errors.
func errMultiClient(err error, method string) error {
	return fmt.Errorf("multi-client request -> %s: %w", method, err)
}
