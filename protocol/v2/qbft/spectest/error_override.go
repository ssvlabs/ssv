package qbft

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// expectedErrorOverride contains mapping testName -> ("spec error" -> "error override substring(s)")
// that will be used to find and replace (if any) the error spectest expects (ExpectedError string)
// with a set of override substring(s) to match the actual error against.
// This mapping allows us to manually track/approve the exact differences between what spec
// tests expect vs what we actually have in our implementation so we can add details to
// our error messages (or use different error message altogether) without accidentally
// missing an important divergence from what spectests expect (which might happen if we don't compare
// error messages at all).
// For simplicity, the overriding pattern is just a bunch of substrings we expect actual error
// to contain.
var expectedErrorOverride = map[string]map[string][]string{}

// validateError checks err against expectedErr, overriding it by a set of patterns to match
// against (defined in expectedErrorOverride) in case test testName has been mapped in this way.
func validateError(t *testing.T, err error, testName string, expectedErr string) {
	if len(expectedErr) == 0 {
		require.NoError(t, err)
		return
	}

	require.Error(t, err, expectedErr)

	wantErrors := []string{expectedErr}
	if testOverride, ok := expectedErrorOverride[testName]; ok {
		if errOverride, ok := testOverride[expectedErr]; ok {
			wantErrors = errOverride
		}
	}
	for _, wantError := range wantErrors {
		require.Contains(t, err.Error(), wantError, fmt.Sprintf("testName: %s", testName))
	}
}
