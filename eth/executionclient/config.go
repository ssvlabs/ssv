package executionclient

import (
	"time"
)

// TODO: rename eth1, consider combining with consensus client options

// Options contains config configurations related to Ethereum execution client.
type Options struct {
	Addr                  string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"Execution client WebSocket URL(s) for eth_subscribe (new head / log streams). Multiple clients are supported via semicolon-separated URLs (e.g. 'ws://localhost:8546;ws://localhost:8547')"`
	QueryAddr             string        `yaml:"ETH1QueryAddr" env:"ETH_1_QUERY_ADDR" env-description:"Optional HTTP URL(s) used for request/response calls (eth_getLogs, eth_blockNumber, eth_getBlockByNumber, ...). When set, large response payloads are routed off the WebSocket transport — sidesteps Besu's StreamBackpressure event-loop blocking on big eth_getLogs replies. URLs are paired positionally with ETH_1_ADDR; falls back to ETH_1_ADDR per-entry when empty. Semicolon-separated, must point at the same physical node as the matching ETH_1_ADDR entry."`
	ConnectionTimeout     time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"Timeout for execution client requests"`
	SyncDistanceTolerance uint64        `yaml:"ETH1SyncDistanceTolerance" env:"ETH_1_SYNC_DISTANCE_TOLERANCE" env-default:"5" env-description:"Maximum number of blocks behind head considered in-sync"`
}
