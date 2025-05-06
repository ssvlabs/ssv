package goclient

import (
	"context"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

// Options defines beacon client options
type Options struct {
	Context                     context.Context
	Network                     beacon.Network
	BeaconNodeAddr              string `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true" env-description:"Beacon node URL(s). Multiple nodes are supported via semicolon-separated URLs (e.g. 'http://localhost:5052;http://localhost:5053')"`
	SyncDistanceTolerance       uint64 `yaml:"SyncDistanceTolerance" env:"BEACON_SYNC_DISTANCE_TOLERANCE" env-default:"4" env-description:"Maximum number of slots behind head considered in-sync"`
	WithWeightedAttestationData bool   `yaml:"WithWeightedAttestationData" env:"WITH_WEIGHTED_ATTESTATION_DATA" env-default:"false" env-description:"Enable attestation data scoring across multiple beacon nodes"`
	WithParallelSubmissions     bool   `yaml:"WithParallelSubmissions" env:"WITH_PARALLEL_SUBMISSIONS" env-default:"false" env-description:"Enables parallel Attestation and Sync Committee submissions to all Beacon nodes (as opposed to submitting to a single Beacon node via multiclient instance)"`

	CommonTimeout time.Duration // Optional.
	LongTimeout   time.Duration // Optional.
}
