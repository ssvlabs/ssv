[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV - Metrics

### Mini Design

`GET /metrics` end-point is exposing metrics from ssv node to prometheus, and later on to be visualized in grafana.

The metrics will be collected on demand, triggered by an HTTP request and processed by the `Handler`,
which then invokes agents that reside in each package that needs to expose some metrics. \
Agents implements `Collector` interface, and needs to be registered by the containing package  
(e.g. `controller` in `validator` package).

<img src="../docs/resources/metrics-collector.png" >

### Collected Metrics

#### Validator

* `count_validators{}`
* `all_connected_peers{}`
* `validator_connected_peers{pubKey}`
* `validator_ibft_status{pubKey,role,seqNumber}`
