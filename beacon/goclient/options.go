package goclient

import (
	"context"
	"time"
)

// Options for controller struct creation
type Options struct {
	Context               context.Context
	BeaconNodeAddr        string        `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true" env-description:"Beacon node address. Supports multiple comma-separated addresses'"`
	SyncDistanceTolerance uint64        `yaml:"SyncDistanceTolerance" env:"BEACON_SYNC_DISTANCE_TOLERANCE" env-default:"4" env-description:"The number of out-of-sync slots we can tolerate"`
	CommonTimeout         time.Duration // Optional.
	LongTimeout           time.Duration // Optional.
}
