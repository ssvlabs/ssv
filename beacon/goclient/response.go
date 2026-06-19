package goclient

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
)

// nilResponseErr returns the unwrapped "<subject> response[ data] is nil" error shared
// by the typed response validators below, or nil when neither the response nor its data
// is missing. Callers wrap the result with errMultiClient/errSingleClient to attach the
// route name (and, for single-client calls, the client address).
func nilResponseErr(respNil, dataNil bool, subject string) error {
	switch {
	case respNil:
		return fmt.Errorf("%s response is nil", subject)
	case dataNil:
		return fmt.Errorf("%s response data is nil", subject)
	default:
		return nil
	}
}

// checkPtrResponse returns a non-nil error if resp or its pointer Data is nil.
func checkPtrResponse[T any](resp *api.Response[*T], subject string) error {
	return nilResponseErr(resp == nil, resp != nil && resp.Data == nil, subject)
}

// checkSliceResponse returns a non-nil error if resp or its slice Data is nil.
func checkSliceResponse[T any](resp *api.Response[[]T], subject string) error {
	return nilResponseErr(resp == nil, resp != nil && resp.Data == nil, subject)
}

// checkMapResponse returns a non-nil error if resp or its map Data is nil.
func checkMapResponse[K comparable, V any](resp *api.Response[map[K]V], subject string) error {
	return nilResponseErr(resp == nil, resp != nil && resp.Data == nil, subject)
}
