[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV - Metrics

`GET /metrics` end-point is exposing metrics from ssv node to prometheus.

### Design

`MetricsAPIPort` is used to enable metrics collection and expose the API: 

Example:
```yaml
MetricsAPIPort: 15001
```

Or as env variable:
```shell
METRICS_API_PORT=15001
```

### Collected Metrics

The following is a list of all collected metrics in SSV:

* `go_*` metrics from `prometheus` 
* `process_*` metrics from `prometheus` 
* `ssv:validator:connected_peers{pubKey} COUNT`
* `ssv:validator:running_ibfts_count{pubKey} COUNT`
* `ssv:validator:running_ibfts_count_all{} COUNT`
* `ssv:validator:validators_count{} COUNT`

### Usage

#### Metrics

```shell
$ curl http://localhost:15000/metrics
```

#### Profiling

Profiling needs to be enabled via config:
```yaml
EnableProfile: true
```

Then all `pprof` routes will be available:
```shell
$ curl http://localhost:15000/debug/pprof/goroutine?seconds=40 --output goroutines.tar.gz
$ go tool pprof goroutines.tar.gz
```
Or in web view:
```shell
$ go tool pprof -web goroutines.tar.gz
```

##### Web UI

To visualize results in web UI directly:
```shell
$ go tool pprof -web http://localhost:15001/debug/pprof/heap?minutes=5
```

##### Pull from stage

```shell
$ curl http://18.237.221.242:15000/metrics --output metrics.out
$ curl http://18.237.221.242:15000/debug/pprof/goroutine\?minutes\=20 --output goroutines.tar.gz
$ curl http://18.237.221.242:15000/debug/pprof/heap\?minutes\=20 --output heap.tar.gz
```
```shell
$ go tool pprof -web goroutines.tar.gz 
$ go tool pprof -web heap.tar.gz
```

#### Health Check

Health check route is available on `GET /health`. \
In case the node is healthy it returns an HTTP Code `200` with empty JSON:
```shell
$ curl http://localhost:15000/health
{}
```

If the node is not healthy, the corresponding errors will be returned with HTTP Code `500`:
```shell
$ curl http://localhost:15000/health
{"errors": ["could not sync eth1 events"]}
```
