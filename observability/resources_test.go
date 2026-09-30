package observability

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.uber.org/zap"
)

// An otel/sdk bump moves resource.Default() to a newer semconv schema; if the
// semconv import in resources.go is not moved with it, the merge fails and the
// node exits at startup (issue #3020).
func TestBuildResources(t *testing.T) {
	require.Equal(t, resource.Default().SchemaURL(), semconv.SchemaURL,
		"semconv import is out of sync with otel/sdk's resource.Default()")

	res, err := buildResources("ssv-node", "v0.0.0", zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, semconv.SchemaURL, res.SchemaURL())
}
