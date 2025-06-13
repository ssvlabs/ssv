# Traces

SSV Node implements the OpenTelemetry specification for traces.  
It supports all standard OTel environment variables, as well as one SSV Node–specific environment variable to enable or disable traces.

When traces are enabled, the `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` ([docs](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/#otel_exporter_otlp_traces_endpoint)) environment variable **must** be set.  
Depending on the selected protocol (`http` or `grpc`), you may also need to set `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` ([docs](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/#otel_exporter_otlp_traces_protocol)).

For a complete list of supported environment variables, refer to the [OpenTelemetry documentation](https://opentelemetry.io/docs).

## Configuration

- **Enable Traces**  
  - **Env var:** `ENABLE_TRACES`  
  - **YAML:** `EnableTraces`  
  - **Type:** `boolean`  
  - **Default:** `false`

### Example configuration

```bash
ENABLE_TRACES=true
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://alloy.observability.svc:4317
OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc
```