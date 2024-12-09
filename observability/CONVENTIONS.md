# Observability Semantic Conventions

The conventions described in this document are based on the [OpenTelemetry documentation](https://opentelemetry.io/docs/).

## Metrics

- **Unit Specification**: Units **MUST** be specified using the `.WithUnit()` method.

### Metric Naming Conventions
[OpenTelemetry documentation](https://opentelemetry.io/docs/specs/semconv/general/metrics)

- **Lowercase Letters**: Metric names **MUST** be in lowercase.
- **Component Delimiter**: A dot (`.`) **MUST** be used as the delimiter between components.
- **Namespace Inclusion**: Metric names **MUST** include a namespace that defines the higher-level component and its position within the object hierarchy. The namespace **MUST** begin with `ssv`, indicating metric ownership.
- **Word Separation**: _Within a single hierarchy level_, words **MUST** be separated by underscores (`_`). (e.g., `ssv.operator.duty_scheduler.executions`)
- **Format Structure**: Metric names **SHOULD** follow the format: `ssv.<domain>.<component>.<metric>`. (e.g., `ssv.operator.duty.executions`)
- **Unit Exclusion from Name**: Metric names **MUST NOT** include the unit of measurement. The `.WithUnit()` method **MUST** be used to specify units.
- **Avoiding `total` Suffix**: The `total` suffix **SHOULD NOT** be used.
- **Counter Naming**: Counters **SHOULD NOT** include a `.count` suffix. Pluralization **MAY** be used (e.g., `ssv.operator.duty.executions`). [See explanation](https://github.com/open-telemetry/opentelemetry-specification/issues/3457).
- **UpDownCounter Naming**: UpDownCounters **SHOULD** include a `.count` suffix (e.g., `ssv.operator.duty.execution.count`). [See explanation](https://github.com/open-telemetry/opentelemetry-specification/issues/3457).
- **Pluralization**: [OTeL documentation](https://opentelemetry.io/docs/specs/semconv/general/metrics/#pluralization)

### Examples of idiomatic metric names

`rpc.server.duration` - Histogram

`messaging.client.operation.duration` - Histogram

`rpc.server.request.size` - Histogram

`db.client.connection.idle.max` - UpDownCounter

`db.client.connection.pending_requests` - UpDownCounter

`db.client.connection.timeouts` - Counter

### Metric Attribute(label) Conventions

- **High Cardinality**: High cardinality attributes are attributes with a large number of unique values. There isn’t a strict definition of how many is “too many,” but generally, 1–10 unique attribute values is considered normal. Attribute values like Peer IDs, IP addresses, and similar, depending on the context, are often high cardinality attributes and should be avoided. How problematic are they? Extremely problematic. High cardinality attributes are expensive to store, query, ingest, and process.
- **Lowercase Letters**: Metric attribute names **MUST** be in lowercase.
- **Component Delimiter**: A dot (`.`) **MUST** be used as the delimiter between components.
- **Namespace** Metric attributes **SHOULD** be added under the metric namespace _when their usage and semantics are exclusive to the metric._ Otherwise the namespace should indicate the domain attributes belongs to. Example: `ethereum.beacon.role`

## Documentation
[Metric attributes](https://opentelemetry.io/docs/specs/semconv/general/metrics/#metric-attributes)

[Instrumentation selection](https://opentelemetry.io/docs/specs/otel/metrics/supplementary-guidelines/#guidelines-for-instrumentation-library-authors)  
