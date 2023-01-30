[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV - Monitoring

`/metrics` end-point is exposing metrics from ssv node to prometheus.

Prometheus should also hit `/health` end-point in order to collect the health check metrics. \
Even if prometheus is not configured, the end-point can simply be polled by a simple HTTP client 
(it doesn't contain metrics)

See the configuration of a [local prometheus service](prometheus/prometheus.yaml).

### Health Check

Health check route is available on `GET /health`. \
In case the node is healthy it returns an HTTP Code `200` with empty response:
```shell
$ curl http://localhost:15000/health
```

If the node is not healthy, the corresponding errors will be returned with HTTP Code `500`:
```shell
$ curl http://localhost:15000/health
{"errors": ["could not sync eth1 events"]}
```

## Metrics

`MetricsAPIPort` is used to enable prometheus metrics collection:

Example:
```yaml
MetricsAPIPort: 15000
```

Or as env variable:
```shell
METRICS_API_PORT=15000
```


## Grafana

In order to setup a grafana dashboard do the following:
1. Enable metrics (`MetricsAPIPort`)
2. Setup Prometheus as mentioned in the beginning of this document and add as data source
    * Job name assumed to be '`ssv`'
3. Import dashboards to Grafana:
   * [SSV Node dashboard](./grafana/NODE.md) 
   * [Operator Performance dashboard](./grafana/PERF.md)
4. Align dashboard variables:
    * `instance` - container name, used in 'instance' field for metrics coming from prometheus. \
      In the given dashboard, instances names are: `ssv-node-v2-<i>`, make sure to change according to your setup

<br />

### Profiling

Profiling can be enabled via config:
```yaml
EnableProfile: true
```

All the default `pprof` routes are available via HTTP:
```shell
$ curl http://localhost:15000/debug/pprof/goroutine?minutes\=20 --output goroutines.tar.gz
```

Open with Go CLI:
```shell
$ go tool pprof goroutines.tar.gz
```

Or with Web UI:
```shell
$ go tool pprof -web goroutines.tar.gz
```

Another option is to visualize results in web UI directly:
```shell
$ go tool pprof -web http://localhost:15001/debug/pprof/heap?minutes=5
```
