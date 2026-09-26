package observability

import (
	"fmt"
	"os"

	"go.opentelemetry.io/otel/sdk/resource"
	// Keep aligned with the schema version used by otel/sdk's resource.Default()
	// (it advances when otel/sdk is bumped); a mismatch makes the resource.Merge
	// below fail with ErrSchemaURLConflict and aborts node startup (issue #3020).
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.uber.org/zap"
)

func buildResources(appName, appVersion string, logger *zap.Logger) (*resource.Resource, error) {
	const defaultHostname = "unknown"

	hostName, err := os.Hostname()
	if err != nil {
		logger.Warn("fetching hostname returned an error. Setting hostname to default",
			zap.Error(err),
			zap.String("default_hostname", defaultHostname))
		hostName = defaultHostname
	}

	const errMsg = "failed to merge OTel Resources"
	resources, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(appName),
		semconv.ServiceVersion(appVersion),
		semconv.HostName(hostName),
	))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errMsg, err)
	}

	resources, err = resource.Merge(resources, resource.Environment())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errMsg, err)
	}

	return resources, nil
}
