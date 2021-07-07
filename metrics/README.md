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


### Collected Metrics

The following is a list of all collected metrics in SSV, grouped by containing package:

#### Validator

* `count_validators{}`
* `all_connected_peers{}`
* `validator_connected_peers{pubKey}`
* `validator_ibft_status{pubKey,role,seqNumber}`
