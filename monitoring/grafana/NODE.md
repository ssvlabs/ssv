[<img src="../docs/resources/ssv_header_image.png" >](https://www.ssvlabs.io/)

<br>
<br>


# SSV Monitoring: Node Dashboard

Shows metrics for general information and health of the SSV node.

See [JSON file](./dashboard_ssv_node.json) to import.

The dashboard consists of the following sections:

### Node Health

**Row 1:**
* Health status for the ssv node (up | error | down): `ssv_node_status{}` (gauge)
* Health status for eth1 (ok | syncing | disconnected): `ssv_eth1_status{}` (gauge)
* Health status for the beacon node (ok | syncing | disconnected): `ssv_beacon_status{}` (gauge)


* Health of the execution client: `ssv_eth1_status{}` (time-series)
* Health of the consensus client: `ssv_beacon_status{}` (time-series)
* Health of the SSV node: `ssv_node_status{}` (time-series)

**Row 2:**
* TODO: Last ETH1 synced block: `ssv_eth1_last_synced_block{} = <block_number>` (gauge)
* TODO: ETH1 registry events: `ssv_eth1_registry_event{name=<event_name>} = <counter>` (gauge bar with counters)

### Process Health

**Row 1:**
* Go memory (released | idle | in-use | heap-sys-bytes): `go_memstats_sys_bytes{}`, `go_memstats_heap_idle_bytes{}`, `go_memstats_heap_inuse_bytes{}`, `go_memstats_stack_inuse_bytes{}` (time-series)
* Kubernetes memory usage (Ram): `container_memory_working_set_bytes{}` (time-series)

**Row 2:**
* Amount of running go routines: `go_goroutines{}` (time-series)
* Kubernetes cpu usage: `container_cpu_usage_seconds_total{}` (time-series)

**Row 3:**
* Kubernetes disk usage (HDD): `kubelet_volume_stats_used_bytes{}` (time-series)
* Kubernetes network I/O usage: `container_network_receive_bytes_total{}`, `container_network_transmit_bytes_total{}` (time-series)

### Network Discovery

**Row 1:**
* Amount of connected peers: `ssv_p2p_connected_peers{} = <gauge>` (time-series)
* Distribution of the topic peers (pubsub): `ssv_p2p_pubsub_topic_peers{topic} = <gauge>` (time-series)

**Row 2:**
* Subnet peers count: `ssv_p2p_dht_subnet_peers{topic} = <gauge>` (table - subnet, subscribed, known peers, connected peers)
* Peers discovery rates: `ssv_p2p_dht_peers_found{} = <counter>`, `ssv_p2p_dht_peers_rejected{} = <counter>` (time-series)

**Row 3:**
* Node info: `ssv_p2p_node_info` (table - index, name, node version, node type, peer ID, last seen)

### Network Messaging

**Row 1:**
* Amount of pubsub messages (in / in by message type / out): `ssv_p2p_pubsub_msg{dir=out|in,topic=*}`, `ssv_p2p_pubsub_msg_invalid{topic}` (time-series)
* Rate of incoming topic messages (pubsub): `ssv_p2p_pubsub_msg{dir=in,topic=<topic_name>} = <counter>` (time-series)
* Rate of outgoing topic messages (pubsub): `ssv_p2p_pubsub_msg{dir=out,topic=<topic_name>} = <counter>` (time-series)

**Row 2:**
* Stream messages (table) (requests | responses | active): `ssv_p2p_stream_msg{dir=out|in,protocol=*}` (table)
* Stream messages (time-series) (requests | responses | active): `ssv_p2p_stream_msg{dir=out|in,protocol=*}` (time-series)
