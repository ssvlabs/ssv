package config

import (
	"fmt"
	"time"
)

// Config defines configuration for the SSV-hosted Builder API endpoint.
//
// This is intentionally a standalone top-level config tree (see `yaml:"builder"` in `cli/operator/node.go`)
// to avoid mixing it with beacon/EL/SSV configs.
type Config struct {
	Enabled       bool   `yaml:"Enabled" env:"ENABLED" env-default:"false" env-description:"Enable the SSV-hosted Builder API endpoint (mev-boost-compatible)"`
	ListenAddress string `yaml:"ListenAddress" env:"LISTEN_ADDRESS" env-description:"Listen address for the builder endpoint (e.g. 127.0.0.1:18550)"`

	// Relay addresses (HTTP) to query for bids/unblind. These should be mev-boost-compatible relay URLs.
	Relays []string `yaml:"Relays" env:"RELAYS" env-description:"Comma-separated list of relay URLs"`

	// RelayRequestTimeout is the per-request timeout for relay HTTP calls.
	RelayRequestTimeout time.Duration `yaml:"RelayRequestTimeout" env:"RELAY_REQUEST_TIMEOUT" env-default:"500ms" env-description:"Timeout for relay HTTP requests"`

	// BidDeadline is the time after slot start at which we stop polling relays for headers.
	BidDeadline time.Duration `yaml:"BidDeadline" env:"BID_DEADLINE" env-default:"850ms" env-description:"Deadline after slot start to stop polling relays for bids"`

	// BidGap is the sleep between consecutive polls to the same relay.
	BidGap time.Duration `yaml:"BidGap" env:"BID_GAP" env-default:"50ms" env-description:"Gap between repeated bid polls per relay"`

	// CacheTTL controls how long we keep bids/provenance in memory.
	CacheTTL time.Duration `yaml:"CacheTTL" env:"CACHE_TTL" env-default:"4s" env-description:"TTL for cached bids (slot-scoped eviction)"`

	// CacheCleanupInterval controls how often we proactively remove expired entries from the in-memory cache.
	//
	// Cache eviction also happens lazily on reads, but prefetching can populate the cache with keys that are
	// never subsequently requested, so a periodic cleanup avoids unbounded growth of expired entries.
	CacheCleanupInterval time.Duration `yaml:"CacheCleanupInterval" env:"CACHE_CLEANUP_INTERVAL" env-default:"1s" env-description:"Interval for proactive cleanup of expired cache entries"`

	// PrefetchEnabled controls whether the node will try to warm bids during proposer duty execution.
	// Note: this does not affect serving the Builder API; it only affects internal prefetch triggers.
	PrefetchEnabled bool `yaml:"PrefetchEnabled" env:"PREFETCH_ENABLED" env-default:"true" env-description:"Enable internal bid prefetching during proposer duties"`

	// PrefetchParentHashTimeout bounds how long we wait when querying the execution client for the current head hash.
	PrefetchParentHashTimeout time.Duration `yaml:"PrefetchParentHashTimeout" env:"PREFETCH_PARENT_HASH_TIMEOUT" env-default:"150ms" env-description:"Timeout for fetching execution head hash used as parent_hash for prefetch"`

	// PrefetchLeadTime is how long before slot start we begin prefetching relay bids.
	// Keep this small to avoid long-running relay polling.
	PrefetchLeadTime time.Duration `yaml:"PrefetchLeadTime" env:"PREFETCH_LEAD_TIME" env-default:"200ms" env-description:"How long before slot start to begin bid prefetching"`

	// PrefetchMaxInFlight bounds in-flight prefetch goroutines.
	PrefetchMaxInFlight int `yaml:"PrefetchMaxInFlight" env:"PREFETCH_MAX_IN_FLIGHT" env-default:"32" env-description:"Maximum in-flight prefetches"`

	// UnblindRetries controls per-relay retries for unblinding.
	UnblindRetries       int           `yaml:"UnblindRetries" env:"UNBLIND_RETRIES" env-default:"0" env-description:"Number of per-relay unblind retries"`
	UnblindRetryInterval time.Duration `yaml:"UnblindRetryInterval" env:"UNBLIND_RETRY_INTERVAL" env-default:"250ms" env-description:"Delay between unblind retries"`

	// UnblindProvenanceHeadStart controls how long the provenance relay gets to unblind before we fall back.
	UnblindProvenanceHeadStart time.Duration `yaml:"UnblindProvenanceHeadStart" env:"UNBLIND_PROVENANCE_HEAD_START" env-default:"25ms" env-description:"Head start for the provenance relay before falling back to other relays for unblinding"`
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.ListenAddress == "" {
		return fmt.Errorf("builder endpoint enabled but ListenAddress is empty")
	}
	return nil
}
