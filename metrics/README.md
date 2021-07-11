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
$ curl http://localhost:15001/metrics
```

Example output:

```
ssv-collect.process.completed_gc_cycles{lastGCTime="1.551963"} 19
ssv-collect.process.cpus_count{} 4
ssv-collect.process.go_version{} go1.15.13
ssv-collect.process.goroutines_count{} 214
ssv-collect.process.memory_stats{alloc="156.281696",sys="355.054624",heapSys="329.187328"} 1
ssv-collect.validator.all_connected_peers{} 3
ssv-collect.validator.connected_peers{pubKey="82e9b36feb8147d3f82c1a03ba246d4a63ac1ce0b1dabbb6991940a06401ab46fb4afbf971a3c145fdad2d4bddd30e12"} 3
ssv-collect.validator.connected_peers{pubKey="8ed3a53383a2c9b9ab0ab5437985ac443a8d50bf50b5f69eeaf9850285aeaad703beff14e3d15b4e6b5702f446a97db4"} 3
ssv-collect.validator.connected_peers{pubKey="afc37265308c7adf734e6d2358bf2458943ee4b2c8598f115c434ea801f13dfa4706efde6c468b0979372d9cd61b14f7"} 3
ssv-collect.validator.count_validators{} 3
ssv-collect.validator.ibft_instance_state_115{identifier="82e9b36feb8147d3f82c1a03ba246d4a63ac1ce0b1dabbb6991940a06401ab46fb4afbf971a3c145fdad2d4bddd30e12_ATTESTER",stage="Prepare",round="2"} 115
ssv-collect.validator.ibft_instance_state_121{identifier="afc37265308c7adf734e6d2358bf2458943ee4b2c8598f115c434ea801f13dfa4706efde6c468b0979372d9cd61b14f7_ATTESTER",stage="Prepare",round="1"} 121
ssv-collect.validator.running_ibfts_count_all{} 2
ssv-collect.validator.running_ibfts_count_validator{pubKey="82e9b36feb8147d3f82c1a03ba246d4a63ac1ce0b1dabbb6991940a06401ab46fb4afbf971a3c145fdad2d4bddd30e12"} 1
ssv-collect.validator.running_ibfts_count_validator{pubKey="8ed3a53383a2c9b9ab0ab5437985ac443a8d50bf50b5f69eeaf9850285aeaad703beff14e3d15b4e6b5702f446a97db4"} 0
ssv-collect.validator.running_ibfts_count_validator{pubKey="afc37265308c7adf734e6d2358bf2458943ee4b2c8598f115c434ea801f13dfa4706efde6c468b0979372d9cd61b14f7"} 1
```


#### Profiling

```shell
$ curl http://localhost:15001/debug/pprof/heap?seconds=10 --output heap.tar.gz
$ go tool pprof heap.tar.gz
```

To visualize results in web
```shell
go tool pprof -web http://localhost:15001/debug/pprof/heap?seconds=10
```