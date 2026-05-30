package operator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Test_run_invalidConfigReturns verifies run() returns the configuration error instead of
// calling logger.Fatal (which would os.Exit the test process) when resolveAndValidate fails.
// A conflicting signing config trips resolveAndValidate — the first thing run() does — before
// any network I/O, so the call is safe to make in-process.
func Test_run_invalidConfigReturns(t *testing.T) {
	c := config{}
	c.SSVSigner.Endpoint = testSignerEndpoint
	c.OperatorPrivateKey = testOperatorKey // remote + local signing => rejected by resolveAndValidate

	err := run(context.Background(), &c, zap.NewNop())
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot enable both remote signing")
}
