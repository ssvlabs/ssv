package qbft

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// expectedErrorOverride contains mapping testName -> "error substring(s)" that will be used to
// replace the expected error (ExpectedError string) in spec tests.
// This mapping allows us to manually track/approve the exact differences between what spec
// tests expect against what we actually have in our implementation so we can have details in
// our error messages (or totally different error message altogether) without accidentally
// diverging from the spec (which might happen if we don't compare our impl errors vs spec).
// For simplicity, the overriding pattern is just a bunch of substrings we expect actual error
// to contain.
var expectedErrorOverride = map[string][]string{}

// validateError checks err against expectedErr, overriding it by a set of patterns to match
// against (defined in expectedErrorOverride) in case test testName has been mapped in this way.
func validateError(t *testing.T, err error, testName string, expectedErr string) {
	if len(expectedErr) == 0 {
		require.NoError(t, err)
		return
	}

	require.Error(t, err, expectedErr)

	wantErrors := []string{expectedErr}
	if errOverride, ok := expectedErrorOverride[testName]; ok {
		wantErrors = errOverride
	}
	for _, wantError := range wantErrors {
		require.Contains(t, err.Error(), wantError, fmt.Sprintf("testName: %s", testName))
	}
}
