[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV Monitoring: Node Dashboard

Shows metrics for general information and health of the SSV node.

See [JSON file](./grafana/dashboard_ssv_node.json) to import.

The dashboard consists of the following sections:

### Node Health

Row 1:
* Health Status (gauge bar, ssv+eth1+beacon) \
  `ssv_node_status{}`, `ssv_eth1_status{}` and `ssv_beacon_status{}`
* Execution client health (time-series, ok|syncing|disconnected) \
  `ssv_eth1_status{}`
* Consensus client health (time-series, ok|syncing|disconnected) \
  `ssv_beacon_status{}`
* SSV node health (time-series, up|error|down) \
  `ssv_node_status{}`

Row 2:
* ETH1 last synced block (gauge) \
  `ssv_eth1_last_synced_blocked{} = <block_number>`
* ETH1 registry events (gauge bar with counters) \
  `ssv_eth1_registry_event{name=<event_name>} = <counter>`

### Process Health

**NOTE:** K8S metrics should be exposed in order to show some panels in this section

Row 1:
* Memory (golang) (time-series, GB scale, released|idle|in-use|heap-sys-bytes) \
  `go_memstats_sys_bytes{}`, `go_memstats_heap_idle_bytes{}`, `go_memstats_heap_inuse_bytes{}`
  and `go_memstats_stack_inuse_bytes{}`
* Memory (k8s) (time-series, GB scale) \
  `container_memory_working_set_bytes{}`

Row 2:
* Goroutines (time-series) \
  `kubelet_volume_stats_used_bytes{}`
* CPU (k8s) (time-series) \
  `container_cpu_usage_seconds_total{}`

Row 3:
* Disk (k8s) (time-series, MB scale) \
  `kubelet_volume_stats_used_bytes{}`
* Network I/O (k8s) (time-series, rate) \
  `container_network_receive_bytes_total{}` and `container_network_transmit_bytes_total{}`

### Network Discovery

Row 1:
* Connected peers (time-series) \
  `ssv_p2p_connected_peers{} = <gauge>`
* Topic peers distribution (pubsub) (time-series) \
  `ssv_p2p_pubsub_topic_peers{topic} = <gauge>`

Row 2:
* Subnet peers distribution (dht) (table) \
  `ssv_p2p_dht_subnet_peers{topic} = <gauge>`
* Peers discovery status (time-series) \
  `ssv_p2p_dht_peers_found{} = <counter>` and `ssv_p2p_dht_peers_rejected{} = <counter>`

Row 3:
* Node info (table, version + operator-id + peer-id)

### Network Messaging

Row 1:
* Pubsub messages (time-series, rate, in|out|invalid) \
  `ssv_p2p_pubusb_msg`, and `ssv_p2p_pubusb_msg_invalid{topic}`
* Outgoing pubsub messages (time-series, rate, labeled by topic) \
  `ssv_p2p_pubusb_msg{dir=out,topic=<topic_name>} = <counter>`
* Incoming pubsub messages (time-series, rate, labeled by topic) \
  `ssv_p2p_pubusb_msg{dir=in,topic=<topic_name>} = <counter>`

Row 2:
* Stream messages (time-series, in|out) \
  `ssv_p2p_stream_msg`
* Outgoing stream messages (time-series, rate, labeled by protocol) \
  `ssv_p2p_stream_msg{dir=out,protocol=<protocol_name>} = <counter>`
* Incoming stream messages (time-series, rate, labeled by protocol) \
  `ssv_p2p_stream_msg{dir=in,protocol=<protocol_name>} = <counter>`