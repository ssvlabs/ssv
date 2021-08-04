[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV - Metrics

`GET /metrics` end-point is exposing metrics from ssv node to prometheus, and later on to be visualized in grafana.

### Design

The metrics will be collected on demand, triggered by an HTTP request and processed by the `Handler`,
which then invokes agents (aka `collectors`) that reside in each package that needs to expose some metrics. \
Collectors implements `Collector` interface, and needs to be registered by the containing package.
Once registered, a collector will be invoked on metrics requests.

<img src="../docs/resources/metrics-collector.png" >

`MetricsAPIPort` is used to enable metrics collection and expose the API on the given nodes: 

```
yaml:"MetricsAPIPort" env:"METRICS_API_PORT" description:"port of metrics api"
```

Example:
```
MetricsAPIPort: 15001
```

### Collected Metrics

The following is a list of all collected metrics in SSV, grouped by containing package:

#### Validator

* `ssv-collect.validator.all_connected_peers{} COUNT`
* `ssv-collect.validator.connected_peers{pubKey} COUNT`
* `ssv-collect.validator.count_validators{} COUNT`
* `ssv-collect.validator.ibft_instance_state_<SEQ_NUMBER>{identifier,round,stage} SEQ_NUMBER`
* `ssv-collect.validator.running_ibfts_count_all{} COUNT`
* `ssv-collect.validator.running_ibfts_count_validator{pubKey} COUNT`

#### Process

* `ssv-collect.process.completed_gc_cycles{lastGCTime} COUNT`
* `ssv-collect.process.cpus_count{} COUNT`
* `ssv-collect.process.go_version{} GOVERSION`
* `ssv-collect.process.goroutines_count{} COUNT`
* `ssv-collect.process.memory_stats{alloc,sys,heapSys} 1`

### Usage

#### Metrics

```shell
$ curl http://localhost:15000/metrics
```

#### Profiling

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

Health check route is available on `GET /health`, which will return an empty response in case the node is healthy:
```shell
$ curl http://localhost:15000/health
{}
```
If the node is not healthy, the corresponding error will be returned:
```shell
$ curl http://localhost:15000/health
{"errors": ["could not sync eth1 events"]}
```