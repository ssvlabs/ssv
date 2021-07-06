# SSV - Metrics

`GET /metrics` end-point is exposing metrics from ssv node to prometheus, and later on to be visualized in grafana.

The following metrics will be collected:

* `count_validators{}`
* `all_connected_peers{}`
* `validator_connected_peers{pubKey}`
