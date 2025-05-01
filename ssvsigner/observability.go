package ssvsigner

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName            = "github.com/ssvlabs/ssv/ssvsigner"
	observabilityNamespaceServer = "ssv.signer.server"
	observabilityNamespaceClient = "ssv.signer.client"
)

const (
	opListValidators   = "list_validators"
	opAddValidator     = "add_validator"
	opRemoveValidator  = "remove_validator"
	opSignValidator    = "sign_validator"
	opOperatorIdentity = "operator_identity"
	opSignOperator     = "sign_operator"

	// Remote Signer operations called by server
	opRemoteSignerListKeys       = "remote_signer_list_keys"
	opRemoteSignerImportKeystore = "remote_signer_import_keystore"
	opRemoteSignerDeleteKeystore = "remote_signer_delete_keystore"
	opRemoteSignerValidatorSign  = "remote_signer_validator_sign"
)

var (
	meter = otel.Meter(observabilityName)

	// ssv-signer HTTP server metrics
	httpRequestsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricNameServer("http.requests"),
			metric.WithUnit("{request}"),
			metric.WithDescription("Total number of HTTP requests received by the signer server"),
		))

	httpErrorsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricNameServer("http.errors"),
			metric.WithUnit("{error}"),
			metric.WithDescription("Total number of HTTP errors returned by the signer server"),
		))

	httpDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricNameServer("http.request.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("Duration of HTTP requests handled by the signer server in seconds"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	// ssv-signer remote signer client metrics
	remoteSignerOpCounter = observability.NewMetric(
		meter.Int64Counter(
			metricNameServer("remote_signer.operations"),
			metric.WithUnit("{operation}"),
			metric.WithDescription("Total number of operations sent to the remote signer by the server"),
		))

	remoteSignerOpErrorsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricNameServer("remote_signer.errors"),
			metric.WithUnit("{error}"),
			metric.WithDescription("Total number of errors received from the remote signer by the server"),
		))

	remoteSignerOpDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricNameServer("remote_signer.operation.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("Duration of operations sent to the remote signer by the server in seconds"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	// ssv-signer client metrics
	clientRequestsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricNameClient("client.http.requests"),
			metric.WithUnit("{request}"),
			metric.WithDescription("Total number of HTTP requests sent by the signer client"),
		))

	clientErrorsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricNameClient("client.http.errors"),
			metric.WithUnit("{error}"),
			metric.WithDescription("Total number of HTTP errors encountered by the signer client"),
		))

	clientDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricNameClient("client.http.request.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("Duration of HTTP requests sent by the signer client in seconds"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))
)

// --- Helper Functions ---

func metricNameServer(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespaceServer, name)
}

func metricNameClient(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespaceClient, name)
}

func httpRouteAttribute(route string) attribute.KeyValue {
	// Using semconv standard attribute key if available, otherwise custom
	// semconv.HTTPRoute is available from v1.17.0 onwards
	return attribute.String("http.route", route) // Use custom for now or check semconv version
}

func httpMethodAttribute(method string) attribute.KeyValue {
	return semconv.HTTPRequestMethodKey.String(method)
}

func httpStatusCodeAttribute(statusCode int) attribute.KeyValue {
	return semconv.HTTPResponseStatusCodeKey.Int(statusCode)
}

func remoteSignerOperationAttribute(operation string) attribute.KeyValue {
	return attribute.String("ssv.signer.remote_signer.operation", operation)
}

func clientOperationAttribute(operation string) attribute.KeyValue {
	return attribute.String("ssv.signer.client.operation", operation)
}

// --- Recording Functions ---

func recordHTTPRequest(ctx context.Context, route string, method string, statusCode int, duration time.Duration) {
	attrs := []attribute.KeyValue{
		httpRouteAttribute(route),
		httpMethodAttribute(method),
		httpStatusCodeAttribute(statusCode),
	}

	httpRequestsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	httpDurationHistogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	if statusCode >= 400 {
		httpErrorsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func recordRemoteSignerOperation(ctx context.Context, operation string, err error, duration time.Duration) {
	attrs := []attribute.KeyValue{
		remoteSignerOperationAttribute(operation),
	}

	remoteSignerOpCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	remoteSignerOpDurationHistogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	if err != nil {
		remoteSignerOpErrorsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func recordClientRequest(ctx context.Context, operation string, err error, duration time.Duration) {
	attrs := []attribute.KeyValue{
		clientOperationAttribute(operation),
	}

	clientRequestsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	clientDurationHistogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	if err != nil {
		clientErrorsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}
