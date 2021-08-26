[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV - Monitoring

`/metrics` end-point is exposing metrics from ssv node to prometheus.

Prometheus should also hit `/health` end-point. \
But if not configured, the end-point can be invoked as it is simpler and doesn't contain metrics.

See the configuration of a [local prometheus service](../prometheus/prometheus.yaml) 

### Collected Metrics

The following is a list of all collected metrics in SSV:

* `go_*` metrics by `prometheus`
* `ssv:node_status` Health check status of operator node
* `ssv:eth1:node_status` Health check status of eth1 node
* `ssv:beacon:node_status` Health check status of beacon node
* `ssv:network:connected_peers{pubKey}` Count connected peers for a validator
* `ssv:network:ibft_decided_messages_outbound{topic}` Count IBFT decided messages outbound
* `ssv:network:ibft_messages_outbound{topic}` Count IBFT messages outbound
* `ssv:network:net_messages_inbound{topic}` Count incoming network messages
* `ssv:validator:ibft_highest_decided{lambda}` The highest decided sequence number
* `ssv:validator:ibft_round{lambda}` IBFTs round
* `ssv:validator:ibft_stage{lambda}` IBFTs stage
* `ssv:validator:ibft_current_slot{pubKey}` Current running slot
* `ssv:validator:running_ibfts_count{pubKey}` Count running IBFTs by validator pub key
* `ssv:validator:running_ibfts_count_all` Count all running IBFTs

### Usage

#### Metrics

`MetricsAPIPort` is used to enable prometheus metrics collection:

Example:
```yaml
MetricsAPIPort: 15000
```

Or as env variable:
```shell
METRICS_API_PORT=15000
```

`<node>:15000/metrics` should be the target for prometheus.

#### Health Check

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
