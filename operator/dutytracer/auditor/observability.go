package auditor

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/dutytracer/auditor"
	observabilityNamespace = "ssv.dutytracer.auditor"
)

var (
	meter = otel.Meter(observabilityName)

	findingsTotal = metrics.New(
		meter.Int64Counter(
			metricName("findings.total"),
			metric.WithDescription("total number of auditor findings emitted"),
		),
	)

	droppedFindings = metrics.New(
		meter.Int64Counter(
			metricName("findings.dropped"),
			metric.WithDescription("total number of auditor findings dropped (cap or store errors)"),
		),
	)

	lastAuditedSlotGauge = metrics.New(
		meter.Int64Gauge(
			metricName("last_audited_slot"),
			metric.WithDescription("last slot successfully audited (best-effort)"),
		),
	)

	attesterRPCRequests = metrics.New(
		meter.Int64Counter(
			metricName("rpc.attester.requests"),
			metric.WithDescription("number of attester RPC fallback requests"),
		),
	)
	attesterRPCErrors = metrics.New(
		meter.Int64Counter(
			metricName("rpc.attester.errors"),
			metric.WithDescription("number of attester RPC fallback errors"),
		),
	)
	syncRPCRequests = metrics.New(
		meter.Int64Counter(
			metricName("rpc.sync.requests"),
			metric.WithDescription("number of sync committee RPC fallback requests"),
		),
	)
	syncRPCErrors = metrics.New(
		meter.Int64Counter(
			metricName("rpc.sync.errors"),
			metric.WithDescription("number of sync committee RPC fallback errors"),
		),
	)
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func reasonAttr(reason string) metric.AddOption {
	return metric.WithAttributes(attribute.String(observability.InstrumentName(observabilityNamespace, "reason"), reason))
}

func dropWhyAttr(why string) metric.AddOption {
	return metric.WithAttributes(attribute.String(observability.InstrumentName(observabilityNamespace, "drop_why"), why))
}
