package observability

import (
	"fmt"
	"os"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.uber.org/zap"
)

func buildResources(appName, appVersion string) (*resource.Resource, error) {
	const defaultHostname = "unknown"

	hostName, err := os.Hostname()
	if err != nil {
		logger.Warn("fetching hostname returned an error. Setting hostname to default",
			zap.Error(err),
			zap.String("default_hostname", defaultHostname))
		hostName = defaultHostname
	}

	const errMsg = "failed to merge OTeL Resources"
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
