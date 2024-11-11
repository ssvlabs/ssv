# Observability Semantic Conventions

The conventions described in this document are based on the [official OpenTelemetry documentation](https://opentelemetry.io/docs/).

## Metrics

- **Unit Specification**: Units **MUST** be specified using the `.WithUnit()` method.

### Metric Naming Conventions

- **Lowercase Letters**: Metric names **MUST** be in lowercase.
- **Component Delimiter**: A dot (`.`) **MUST** be used as the delimiter between components.
- **Namespace Inclusion**: Metric names **MUST** include a namespace that defines the higher-level component and its position within the object hierarchy. The namespace **MUST** begin with `ssv`, indicating metric ownership.
- **Word Separation**: _Within a single hierarchy level_, words **MUST** be separated by underscores (`_`).
- **Format Structure**: Metric names **SHOULD** follow the format: `ssv.<domain>.<component>.<metric>`. (e.g., `ssv.operator.duty.executions`)
- **Unit Exclusion from Name**: Metric names **MUST NOT** include the unit of measurement. The `.WithUnit()` method **MUST** be used to specify units.
- **Avoiding `total` Suffix**: The `total` suffix **SHOULD NOT** be used.
- **Counter Naming**: Counters **SHOULD NOT** include a `.count` suffix. Pluralization **MAY** be used (e.g., `ssv.operator.duty.executions`). [See explanation](https://github.com/open-telemetry/opentelemetry-specification/issues/3457).
- **UpDownCounter Naming**: UpDownCounters **SHOULD** include a `.count` suffix (e.g., `ssv.operator.duty.execution.count`). [See explanation](https://github.com/open-telemetry/opentelemetry-specification/issues/3457).
- **Pluralization**: [OTeL documentation](https://opentelemetry.io/docs/specs/semconv/general/metrics/#pluralization)

### Metric Attribute Conventions