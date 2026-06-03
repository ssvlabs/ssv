package observability

import (
	"fmt"
	"os"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.uber.org/zap"
)

func buildResources(appName, appVersion string, extraAttrs []attribute.KeyValue, logger *zap.Logger) (*resource.Resource, error) {
	const defaultHostname = "unknown"

	hostName, err := os.Hostname()
	if err != nil {
		logger.Warn("fetching hostname returned an error. Setting hostname to default",
			zap.Error(err),
			zap.String("default_hostname", defaultHostname))
		hostName = defaultHostname
	}

	// Build the base attribute set, then append any caller-supplied extras (e.g. operator_id
	// which is only known after the operator data store is initialized).
	const baseAttrCount = 3
	baseAttrs := make([]attribute.KeyValue, 0, baseAttrCount+len(extraAttrs))
	baseAttrs = append(baseAttrs,
		semconv.ServiceName(appName),
		semconv.ServiceVersion(appVersion),
		semconv.HostName(hostName),
	)
	baseAttrs = append(baseAttrs, extraAttrs...)

	const errMsg = "failed to merge OTeL Resources"
	resources, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		baseAttrs...,
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
