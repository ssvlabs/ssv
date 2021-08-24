[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV - Metrics

`GET /metrics` end-point is exposing metrics from ssv node to prometheus.

### Collected Metrics

The following is a list of all collected metrics in SSV:

* `go_*` metrics by `prometheus`
* `ssv:network:connected_peers{pubKey}` Count connected peers for a validator
* `ssv:network:ibft_decided_messages_outbound{topic}` Count IBFT decided messages outbound
* `ssv:network:ibft_messages_outbound{topic}` Count IBFT messages outbound
* `ssv:network:net_messages_inbound{topic}` Count incoming network messages
* `ssv:validator:ibft_highest_decided{lambda}` The highest decided sequence number
* `ssv:validator:ibft_round{lambda}` IBFTs round
* `ssv:validator:ibft_stage{lambda}` IBFTs stage
* `ssv:validator:ibfts_on_init{pubKey}` Count IBFTs in 'init' phase
* `ssv:validator:running_ibfts_count{pubKey}` Count running IBFTs by validator pub key
* `ssv:validator:running_ibfts_count_all` Count all running IBFTs

### Usage

#### Metrics

`MetricsAPIPort` is used to enable metrics collection and expose the API:

Example:
```yaml
MetricsAPIPort: 15000
```

Or as env variable:
```shell
METRICS_API_PORT=15000
```

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
